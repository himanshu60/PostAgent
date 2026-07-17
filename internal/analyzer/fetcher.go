package analyzer

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

type ExampleStore struct {
	coll   *mongo.Collection
	logger *zap.Logger
}

type ExamplePost struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	Body      string    `bson:"body" json:"body"`
	ImageURL  string    `bson:"image_url,omitempty" json:"image_url,omitempty"`
	Source    string    `bson:"source" json:"source"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}

func NewExampleStore(db *mongo.Database, logger *zap.Logger) *ExampleStore {
	return &ExampleStore{
		coll:   db.Collection("agent_examples"),
		logger: logger,
	}
}

func (s *ExampleStore) Insert(ctx context.Context, body, imageURL string) error {
	_, err := s.coll.InsertOne(ctx, ExamplePost{
		Body:      body,
		ImageURL:  imageURL,
		Source:    "user",
		CreatedAt: time.Now(),
	})
	return err
}

func (s *ExampleStore) List(ctx context.Context, limit int) ([]ExamplePost, error) {
	opts := options.Find().SetSort(bson.M{"created_at": -1}).SetLimit(int64(limit))
	cursor, err := s.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	var posts []ExamplePost
	if err := cursor.All(ctx, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

func (s *ExampleStore) DeleteAll(ctx context.Context) error {
	_, err := s.coll.DeleteMany(ctx, bson.M{})
	return err
}
