package surface

import (
	"bytes"
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Repository abstracts surface persistence so tests and future backends
// (S3, etc.) can swap the storage layer.
type Repository interface {
	ListByKind(ctx context.Context, kind Kind) ([]Surface, error)
	GetByID(ctx context.Context, id string) (*Surface, error)
	GetImage(ctx context.Context, id string) ([]byte, string, error)
	Upsert(ctx context.Context, s Surface, imageData []byte, contentType string) error
}

// MongoRepository persists surfaces in MongoDB + GridFS (default bucket,
// matching the wardrobe pattern).
type MongoRepository struct {
	client *mongo.Client
	dbName string
}

// NewMongoRepository constructs a Mongo-backed surface repository.
func NewMongoRepository(client *mongo.Client, dbName string) *MongoRepository {
	return &MongoRepository{client: client, dbName: dbName}
}

func (r *MongoRepository) collection() *mongo.Collection {
	return r.client.Database(r.dbName).Collection("surfaces")
}

func (r *MongoRepository) bucket() *mongo.GridFSBucket {
	return r.client.Database(r.dbName).GridFSBucket()
}

// ListByKind returns every surface of the requested kind. Used during
// outfit generation to hand the LLM its menu of options.
func (r *MongoRepository) ListByKind(ctx context.Context, kind Kind) ([]Surface, error) {
	cur, err := r.collection().Find(ctx, bson.M{"kind": string(kind)})
	if err != nil {
		return nil, fmt.Errorf("surface list: %w", err)
	}
	defer cur.Close(ctx)

	var out []Surface
	if err := cur.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("surface decode: %w", err)
	}
	return out, nil
}

// GetByID fetches a single surface doc. Returns mongo.ErrNoDocuments if absent.
func (r *MongoRepository) GetByID(ctx context.Context, id string) (*Surface, error) {
	var s Surface
	err := r.collection().FindOne(ctx, bson.M{"_id": id}).Decode(&s)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetImage streams the raw image bytes for the given surface ID from GridFS.
// Returns mongo.ErrFileNotFound if the image was never uploaded.
func (r *MongoRepository) GetImage(ctx context.Context, id string) ([]byte, string, error) {
	var buf bytes.Buffer
	if _, err := r.bucket().DownloadToStreamByName(ctx, id, &buf); err != nil {
		return nil, "", err
	}

	var fileDoc struct {
		Metadata struct {
			ContentType string `bson:"contentType"`
		} `bson:"metadata"`
	}
	if err := r.bucket().GetFilesCollection().FindOne(ctx, bson.M{"filename": id}).Decode(&fileDoc); err != nil {
		return buf.Bytes(), "image/png", nil
	}
	ct := fileDoc.Metadata.ContentType
	if ct == "" {
		ct = "image/png"
	}
	return buf.Bytes(), ct, nil
}

// Upsert writes (or replaces) both the metadata document and the image
// bytes. Re-running the seed with the same ID refreshes the stored data
// rather than duplicating it.
func (r *MongoRepository) Upsert(ctx context.Context, s Surface, imageData []byte, contentType string) error {
	// Replace the metadata doc.
	opts := options.Replace().SetUpsert(true)
	if _, err := r.collection().ReplaceOne(ctx, bson.M{"_id": s.ID}, s, opts); err != nil {
		return fmt.Errorf("surface upsert metadata: %w", err)
	}

	// Drop any existing GridFS file under this name before re-uploading —
	// otherwise repeated seeds accumulate orphan chunks.
	bucket := r.bucket()
	var existing struct {
		ID any `bson:"_id"`
	}
	err := bucket.GetFilesCollection().FindOne(ctx, bson.M{"filename": s.ID}).Decode(&existing)
	if err == nil && existing.ID != nil {
		if delErr := bucket.Delete(ctx, existing.ID); delErr != nil {
			return fmt.Errorf("surface: drop stale image: %w", delErr)
		}
	}

	// Upload the fresh bytes.
	uploadOpts := options.GridFSUpload().SetMetadata(bson.M{"contentType": contentType})
	if _, err := bucket.UploadFromStream(ctx, s.ID, bytes.NewReader(imageData), uploadOpts); err != nil {
		return fmt.Errorf("surface upload image: %w", err)
	}
	return nil
}
