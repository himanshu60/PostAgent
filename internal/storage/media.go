package storage

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"

	"go.mongodb.org/mongo-driver/mongo/options"
)

// MediaRepo stores uploaded image bytes in MongoDB so they survive restarts and
// ephemeral-disk deploys (Render/containers), and are available at publish time.
type MediaRepo struct {
	coll *mongo.Collection
}

func NewMediaRepo(db *MongoDB) *MediaRepo {
	return &MediaRepo{coll: db.Media()}
}

type mediaDoc struct {
	ID          string    `bson:"_id"`
	UserID      string    `bson:"user_id,omitempty"`
	ContentType string    `bson:"content_type"`
	Data        []byte    `bson:"data"`
	CreatedAt   time.Time `bson:"created_at"`
}

// Put stores (or replaces) an image blob under id.
func (r *MediaRepo) Put(ctx context.Context, id, userID, contentType string, data []byte) error {
	_, err := r.coll.ReplaceOne(ctx,
		bson.M{"_id": id},
		mediaDoc{ID: id, UserID: userID, ContentType: contentType, Data: data, CreatedAt: time.Now()},
		options.Replace().SetUpsert(true),
	)
	return err
}

// Get returns the content type and bytes for an image id.
func (r *MediaRepo) Get(ctx context.Context, id string) (contentType string, data []byte, err error) {
	var doc mediaDoc
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&doc); err != nil {
		return "", nil, err
	}
	return doc.ContentType, doc.Data, nil
}
