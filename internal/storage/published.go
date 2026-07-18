package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

type PublishedRepo struct {
	coll *mongo.Collection
}

func NewPublishedRepo(db *MongoDB) *PublishedRepo {
	return &PublishedRepo{coll: db.Published()}
}

func (r *PublishedRepo) Insert(ctx context.Context, post *models.PublishedPost) error {
	if post.ID == "" {
		post.ID = uuid.New().String()
	}
	post.PostedAt = time.Now()
	_, err := r.coll.InsertOne(ctx, post)
	return err
}

func (r *PublishedRepo) UpdateAnalytics(ctx context.Context, id string, likes, comments, impressions int) error {
	now := time.Now()
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"likes":                likes,
			"comments":             comments,
			"impressions":          impressions,
			"last_analytics_fetch": &now,
		},
	})
	return err
}

func (r *PublishedRepo) List(ctx context.Context, userID string, limit int) ([]models.PublishedPost, error) {
	opts := options.Find().SetSort(bson.M{"posted_at": -1}).SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	var posts []models.PublishedPost
	if err := cursor.All(ctx, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}

func (r *PublishedRepo) GetRecent(ctx context.Context, userID string, limit int) ([]models.PublishedPost, error) {
	opts := options.Find().SetSort(bson.M{"_id": -1}).SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	var posts []models.PublishedPost
	if err := cursor.All(ctx, &posts); err != nil {
		return nil, err
	}
	return posts, nil
}
