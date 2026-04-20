package moodboard

import (
	"bytes"
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"mootd/backend/internal/shared/pagination"
)

// gridFSFilenamePrefix namespaces moodboard render images in the shared default
// GridFS bucket so they don't collide with wardrobe item images (which use the
// raw item ID as the filename).
const gridFSFilenamePrefix = "moodboard:"

func imageFilename(boardID string) string { return gridFSFilenamePrefix + boardID }

// Repository handles moodboard persistence.
type Repository interface {
	Save(ctx context.Context, board SavedMoodBoard) error
	FindByUser(ctx context.Context, userID string) ([]SavedMoodBoard, error)
	// FindByUserPaginated returns a page of moodboards using cursor-based pagination.
	FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]SavedMoodBoard, error)
	// FindRecent returns the N most recent moodboards for the user, newest first.
	FindRecent(ctx context.Context, userID string, limit int) ([]SavedMoodBoard, error)
	// DeleteAllByUser removes every moodboard (and its rendered image) owned
	// by userID. Used for GDPR account erasure.
	DeleteAllByUser(ctx context.Context, userID string) (int, error)
	// FindByID returns a single moodboard by its _id, or mongo.ErrNoDocuments.
	// Used by the image-serve handler to check existence without leaking which
	// boards belong to which user.
	FindByID(ctx context.Context, id string) (*SavedMoodBoard, error)
	// SaveImage persists the rendered collage PNG under the moodboard ID.
	SaveImage(ctx context.Context, boardID string, data []byte, contentType string) error
	// GetImage retrieves the rendered image bytes + content-type, or
	// mongo.ErrFileNotFound when no render was stored.
	GetImage(ctx context.Context, boardID string) ([]byte, string, error)
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
	return r.client.Database(r.dbName).Collection("moodboards")
}

// gridFSBucket returns the shared default GridFS bucket. We reuse it rather
// than maintaining a moodboard-specific bucket — the filename prefix provides
// enough namespacing, and a single bucket keeps GDPR cleanup simpler.
func (r *MongoRepository) gridFSBucket() *mongo.GridFSBucket {
	return r.client.Database(r.dbName).GridFSBucket()
}

// deleteGridFSByName removes a GridFS file by filename when present. A missing
// file is not an error — image uploads are best-effort and may have failed.
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

// Save upserts a moodboard for the given user+date.
// If an outfit already exists for that date, the old one is deleted first
// to avoid MongoDB's immutable _id constraint on ReplaceOne.
func (r *MongoRepository) Save(ctx context.Context, board SavedMoodBoard) error {
	filter := bson.M{"userId": board.UserID, "date": board.Date}
	if _, err := r.collection().DeleteMany(ctx, filter); err != nil {
		return fmt.Errorf("moodboard: purge existing for user %s date %s: %w", board.UserID, board.Date, err)
	}
	_, err := r.collection().InsertOne(ctx, board)
	return err
}

// FindByUser returns all moodboards for the user, newest first.
func (r *MongoRepository) FindByUser(ctx context.Context, userID string) ([]SavedMoodBoard, error) {
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var boards []SavedMoodBoard
	if err := cursor.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

// FindByUserPaginated returns a page of moodboards for the given user.
// It fetches limit+1 rows so the caller can detect whether a next page exists.
func (r *MongoRepository) FindByUserPaginated(ctx context.Context, userID string, limit int, cursor *pagination.Cursor) ([]SavedMoodBoard, error) {
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

	var boards []SavedMoodBoard
	if err := cur.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

// DeleteAllByUser removes every moodboard owned by userID, including the
// rendered collage image (if any) for each one. Image-delete failures are
// silently tolerated — a missing GridFS file is fine, and a transient error
// must not block the user's account erasure.
func (r *MongoRepository) DeleteAllByUser(ctx context.Context, userID string) (int, error) {
	// Fetch IDs first so GridFS renders keyed by board ID can be cleaned up.
	cur, err := r.collection().Find(ctx, bson.M{"userId": userID}, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return 0, err
	}
	var ids []struct {
		ID string `bson:"_id"`
	}
	if err := cur.All(ctx, &ids); err != nil {
		return 0, err
	}

	bucket := r.gridFSBucket()
	for _, row := range ids {
		_ = r.deleteGridFSByName(ctx, bucket, imageFilename(row.ID))
	}

	res, err := r.collection().DeleteMany(ctx, bson.M{"userId": userID})
	if err != nil {
		return 0, err
	}
	return int(res.DeletedCount), nil
}

// FindByID returns a single moodboard by its _id, or mongo.ErrNoDocuments
// when nothing matched. Used by the public image handler.
func (r *MongoRepository) FindByID(ctx context.Context, id string) (*SavedMoodBoard, error) {
	var board SavedMoodBoard
	if err := r.collection().FindOne(ctx, bson.M{"_id": id}).Decode(&board); err != nil {
		return nil, err
	}
	return &board, nil
}

// SaveImage writes the rendered moodboard PNG to GridFS under a namespaced
// filename. If a previous render exists for the same moodboard it is removed
// first, so uploads are idempotent.
func (r *MongoRepository) SaveImage(ctx context.Context, boardID string, data []byte, contentType string) error {
	bucket := r.gridFSBucket()
	name := imageFilename(boardID)
	if err := r.deleteGridFSByName(ctx, bucket, name); err != nil {
		return fmt.Errorf("replace old moodboard image: %w", err)
	}
	metadata := bson.D{{Key: "contentType", Value: contentType}}
	uploadOpts := options.GridFSUpload().SetMetadata(metadata)
	// Use the filename as the ID too, so moodboards can be deleted by name
	// without a secondary lookup and the two identifiers stay in lockstep.
	return bucket.UploadFromStreamWithID(ctx, name, name, bytes.NewReader(data), uploadOpts)
}

// GetImage retrieves the rendered image bytes and content-type for a saved
// moodboard. Returns mongo.ErrFileNotFound when the client didn't upload a
// render at save time.
func (r *MongoRepository) GetImage(ctx context.Context, boardID string) ([]byte, string, error) {
	bucket := r.gridFSBucket()
	name := imageFilename(boardID)

	var buf bytes.Buffer
	if _, err := bucket.DownloadToStreamByName(ctx, name, &buf); err != nil {
		return nil, "", err
	}

	var fileDoc struct {
		Metadata struct {
			ContentType string `bson:"contentType"`
		} `bson:"metadata"`
	}
	if err := bucket.GetFilesCollection().FindOne(ctx, bson.M{"filename": name}).Decode(&fileDoc); err != nil {
		// Metadata lookup is best-effort; PNG is the safe default for
		// client-captured renders.
		return buf.Bytes(), "image/png", nil
	}
	ct := fileDoc.Metadata.ContentType
	if ct == "" {
		ct = "image/png"
	}
	return buf.Bytes(), ct, nil
}

// FindRecent returns the N most recent moodboards for the user, newest first.
func (r *MongoRepository) FindRecent(ctx context.Context, userID string, limit int) ([]SavedMoodBoard, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "createdAt", Value: -1}}).
		SetLimit(int64(limit))
	cursor, err := r.collection().Find(ctx, bson.M{"userId": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var boards []SavedMoodBoard
	if err := cursor.All(ctx, &boards); err != nil {
		return nil, err
	}
	return boards, nil
}

