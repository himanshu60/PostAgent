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

type TopicRepo struct {
	coll *mongo.Collection
}

func NewTopicRepo(db *MongoDB) *TopicRepo {
	return &TopicRepo{coll: db.Topics()}
}

func (r *TopicRepo) Insert(ctx context.Context, topic *models.Topic) error {
	if topic.ID == "" {
		topic.ID = uuid.New().String()
	}
	topic.CreatedAt = time.Now()
	_, err := r.coll.InsertOne(ctx, topic)
	return err
}

func (r *TopicRepo) InsertMany(ctx context.Context, topics []models.Topic) error {
	if len(topics) == 0 {
		return nil
	}
	for i := range topics {
		if topics[i].ID == "" {
			topics[i].ID = uuid.New().String()
		}
		topics[i].CreatedAt = time.Now()
	}
	docs := make([]interface{}, len(topics))
	for i := range topics {
		docs[i] = topics[i]
	}
	_, err := r.coll.InsertMany(ctx, docs)
	return err
}

func (r *TopicRepo) List(ctx context.Context, limit int) ([]models.Topic, error) {
	opts := options.Find().SetSort(bson.M{"created_at": -1}).SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	var topics []models.Topic
	if err := cursor.All(ctx, &topics); err != nil {
		return nil, err
	}
	return topics, nil
}

// GetUnused returns unused topics from the given source (e.g. "custom" — the
// user's manually-added queue). Source is required: "manual" topics (created
// by the on-demand generate flow) already have a draft the moment they're
// created and are never meant to be picked up again, so they must never leak
// into this queue.
func (r *TopicRepo) GetUnused(ctx context.Context, source string, limit int) ([]models.Topic, error) {
	filter := bson.M{"used": false, "source": source}
	opts := options.Find().SetLimit(int64(limit)).SetSort(bson.M{"created_at": -1})
	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var topics []models.Topic
	if err := cursor.All(ctx, &topics); err != nil {
		return nil, err
	}
	return topics, nil
}

func (r *TopicRepo) MarkUsed(ctx context.Context, ids ...string) error {
	filter := bson.M{"_id": bson.M{"$in": ids}}
	update := bson.M{"$set": bson.M{"used": true}}
	_, err := r.coll.UpdateMany(ctx, filter, update)
	return err
}

func (r *TopicRepo) GetByID(ctx context.Context, id string) (*models.Topic, error) {
	var topic models.Topic
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&topic)
	if err != nil {
		return nil, err
	}
	return &topic, nil
}

func (r *TopicRepo) Delete(ctx context.Context, id string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// RecentTitles returns the titles of the most recently created topics, used
// to check new candidates against everything already covered.
func (r *TopicRepo) RecentTitles(ctx context.Context, limit int) ([]string, error) {
	opts := options.Find().
		SetProjection(bson.M{"title": 1}).
		SetSort(bson.M{"created_at": -1}).
		SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	var docs []struct {
		Title string `bson:"title"`
	}
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	titles := make([]string, len(docs))
	for i, d := range docs {
		titles[i] = d.Title
	}
	return titles, nil
}

func (r *TopicRepo) ExistsByURL(ctx context.Context, url string) (bool, error) {
	count, err := r.coll.CountDocuments(ctx, bson.M{"url": url})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
