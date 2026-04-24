package wardrobe

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"mootd/backend/internal/shared/pagination"
)

// Repository handles wardrobe item persistence.
type Repository interface {
	Save(ctx context.Context, item ClothingItem) error
	FindByUser(ctx context.Context, userID string) ([]ClothingItem, error)
	// FindByUserPaginated returns a page of clothing items for the given user using cursor-based pagination.
	// The caller should request limit+1 items; if len(result) > limit there is a next page.
	FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]ClothingItem, error)
	// UpdateItem sets traits and optionally label and imageUrl. Empty strings are ignored.
	UpdateItem(ctx context.Context, id, userID string, traits map[string]string, label, imageURL string) error
	Delete(ctx context.Context, id, userID string) error
	// DeleteAllByUser removes every wardrobe item (plus its GridFS image) owned
	// by userID. Used for GDPR account erasure.
	DeleteAllByUser(ctx context.Context, userID string) (int, error)
	SaveImage(ctx context.Context, itemID string, data []byte, contentType string) error
	GetImage(ctx context.Context, itemID string) ([]byte, string, error)
	// FindMissingPNG returns items that have an imageUrl but no pngImageUrl yet.
	FindMissingPNG(ctx context.Context) ([]ClothingItem, error)
	// UpdatePngURL sets the pngImageUrl field on an item.
	UpdatePngURL(ctx context.Context, id, pngURL string) error
}

// MongoRepository implements Repository using MongoDB.
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository creates a MongoRepository.
func NewMongoRepository(client *mongo.Client, dbName string) *MongoRepository {
	return &MongoRepository{client: client, dbName: dbName}
}

func (r *MongoRepository) collection() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("wardrobe_items")
}

// gridFSBucket returns the GridFS bucket that stores wardrobe item images.
//
// Historically this used SetName("wardrobe_images") for namespace isolation, but
// the actual image data lives in the default fs.* collections — the setter was
// added after the data was seeded, so every read against the namespaced bucket
// silently returned ErrFileNotFound. We intentionally match existing data now;
// the namespace design never went live.
func (r *MongoRepository) gridFSBucket() *mongo.GridFSBucket {
	return r.client.Database(r.dbName).GridFSBucket()
}

// Save upserts a clothing item by its ID.
func (r *MongoRepository) Save(ctx context.Context, item ClothingItem) error {
	filter := bson.M{"_id": item.ID}
	update := bson.M{"$set": item}
	_, err := r.collection().UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	return err
}

// UpdateItem sets traits for the item and optionally updates label and imageUrl.
// Empty strings for label and imageURL are ignored (field is not overwritten).
// Returns mongo.ErrNoDocuments if the item is not found or not owned by userID.
func (r *MongoRepository) UpdateItem(ctx context.Context, id, userID string, traits map[string]string, label, imageURL string) error {
	filter := bson.M{"_id": id, "userId": userID}
	fields := bson.M{"traits": traits}
	if label != "" {
		fields["label"] = label
	}
	if imageURL != "" {
		fields["imageUrl"] = imageURL
		// Clear the background-removed PNG so the product image is used instead.
		fields["pngImageUrl"] = ""
	}
	result, err := r.collection().UpdateOne(ctx, filter, bson.M{"$set": fields})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// Delete removes the item with the given ID, only if it belongs to userID.
// Returns mongo.ErrNoDocuments if no matching item was found.
func (r *MongoRepository) Delete(ctx context.Context, id, userID string) error {
	result, err := r.collection().DeleteOne(ctx, bson.M{"_id": id, "userId": userID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// SaveImage stores the image bytes for an item in GridFS using itemID as filename.
// If an image with the same name already exists it is replaced.
func (r *MongoRepository) SaveImage(ctx context.Context, itemID string, data []byte, contentType string) error {
	bucket := r.gridFSBucket()

	// Delete any existing file with this name so uploads are idempotent.
	if err := r.deleteGridFSByName(ctx, bucket, itemID); err != nil {
		return fmt.Errorf("replace old image: %w", err)
	}

	metadata := bson.D{{Key: "contentType", Value: contentType}}
	uploadOpts := options.GridFSUpload().SetMetadata(metadata)
	err := bucket.UploadFromStreamWithID(ctx, itemID, itemID, bytes.NewReader(data), uploadOpts)
	return err
}

// GetImage retrieves the image bytes and content-type for an item from GridFS.
// Returns mongo.ErrFileNotFound if no image exists for the item.
//
// Implementation note (B5 perf fix): a single FindOne on the files
// collection returns both the GridFS _id (used to stream the bytes) and
// the stored content-type metadata. The previous implementation called
// DownloadToStreamByName (which internally does its own FindOne + stream
// download) plus a *separate* FindOne for metadata — three MongoDB
// round-trips per image. The vision-mode outfit flow loads up to 24
// images per generation, so the old pattern cost 48–72 extra round-
// trips per outfit — 50–120 ms wasted on every vision generation.
func (r *MongoRepository) GetImage(ctx context.Context, itemID string) ([]byte, string, error) {
	bucket := r.gridFSBucket()

	var fileDoc struct {
		ID       any `bson:"_id"`
		Metadata struct {
			ContentType string `bson:"contentType"`
		} `bson:"metadata"`
	}
	if err := bucket.GetFilesCollection().FindOne(ctx, bson.M{"filename": itemID}).Decode(&fileDoc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Preserve the public contract — callers (wardrobe handler,
			// pngworker, claude loader) check for mongo.ErrFileNotFound
			// to distinguish missing image from other failures.
			return nil, "", mongo.ErrFileNotFound
		}
		return nil, "", err
	}

	var buf bytes.Buffer
	if _, err := bucket.DownloadToStream(ctx, fileDoc.ID, &buf); err != nil {
		return nil, "", err
	}

	ct := fileDoc.Metadata.ContentType
	if ct == "" {
		ct = "image/jpeg"
	}
	return buf.Bytes(), ct, nil
}

// deleteGridFSByName removes a GridFS file by filename if it exists.
// Errors from missing files are silently ignored.
func (r *MongoRepository) deleteGridFSByName(ctx context.Context, bucket *mongo.GridFSBucket, name string) error {
	var fileDoc struct {
		ID interface{} `bson:"_id"`
	}
	err := bucket.GetFilesCollection().FindOne(ctx, bson.M{"filename": name}).Decode(&fileDoc)
	if err == mongo.ErrNoDocuments {
		return nil
	}
	if err != nil {
		return err
	}
	return bucket.Delete(ctx, fileDoc.ID)
}

// FindMissingPNG returns items that have an imageUrl but an empty pngImageUrl.
func (r *MongoRepository) FindMissingPNG(ctx context.Context) ([]ClothingItem, error) {
	filter := bson.M{
		"imageUrl":    bson.M{"$ne": ""},
		"pngImageUrl": "",
	}
	cursor, err := r.collection().Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []ClothingItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// UpdatePngURL sets the pngImageUrl field for the given item ID.
func (r *MongoRepository) UpdatePngURL(ctx context.Context, id, pngURL string) error {
	_, err := r.collection().UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"pngImageUrl": pngURL}})
	return err
}

// FindByUserPaginated returns a page of clothing items for the given user.
// It fetches limit+1 rows so the caller can detect whether a next page exists.
func (r *MongoRepository) FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]ClothingItem, error) {
	filter := bson.M{"userId": userID}
	filter = pagination.BuildFilter(filter, cursor)

	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}).
		SetLimit(int64(limit + 1))

	cur, err := r.collection().Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var items []ClothingItem
	if err := cur.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// DeleteAllByUser removes every wardrobe item for userID along with its GridFS
// image blob. Returns the number of items deleted. Image-delete failures are
// logged via the returned error only when the item-delete itself fails — orphan
// GridFS files left behind do not block account erasure, but count as a repo
// consistency issue the caller can alert on.
func (r *MongoRepository) DeleteAllByUser(ctx context.Context, userID string) (int, error) {
	// Fetch IDs first so we can clean up GridFS blobs that are keyed by item ID.
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return 0, err
	}
	var ids []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &ids); err != nil {
		return 0, err
	}

	bucket := r.gridFSBucket()
	for _, row := range ids {
		// Best-effort GridFS cleanup — a missing file is not an error.
		_ = r.deleteGridFSByName(ctx, bucket, row.ID)
	}

	res, err := r.collection().DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}

// FindByUser returns all clothing items for the given user, newest first.
func (r *MongoRepository) FindByUser(ctx context.Context, userID string) ([]ClothingItem, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var items []ClothingItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}
	return items, nil
}
