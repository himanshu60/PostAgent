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

type CycleRepo struct {
	coll *mongo.Collection
}

func NewCycleRepo(db *MongoDB) *CycleRepo {
	return &CycleRepo{coll: db.Cycles()}
}

func (r *CycleRepo) Insert(ctx context.Context, cycle *models.Cycle) error {
	if cycle.ID == "" {
		cycle.ID = uuid.New().String()
	}
	cycle.StartedAt = time.Now()
	_, err := r.coll.InsertOne(ctx, cycle)
	return err
}

func (r *CycleRepo) Complete(ctx context.Context, id string) error {
	now := time.Now()
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"status": models.CycleStatusCompleted, "ended_at": &now},
	})
	return err
}

func (r *CycleRepo) Skip(ctx context.Context, id string) error {
	now := time.Now()
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"status": models.CycleStatusSkipped, "ended_at": &now},
	})
	return err
}

func (r *CycleRepo) List(ctx context.Context, userID string, limit int) ([]models.Cycle, error) {
	opts := options.Find().SetSort(bson.M{"started_at": -1}).SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return nil, err
	}
	var cycles []models.Cycle
	if err := cursor.All(ctx, &cycles); err != nil {
		return nil, err
	}
	return cycles, nil
}

// HasRunSince reports whether a cycle with the given trigger has started at
// or after `since`. Used to make the daily auto-generate job idempotent and
// self-healing: check "did today's run already happen?" and only run it if
// not — catching up after sleep/downtime instead of silently skipping days.
func (r *CycleRepo) HasRunSince(ctx context.Context, userID, trigger string, since time.Time) (bool, error) {
	count, err := r.coll.CountDocuments(ctx, bson.M{
		"user_id":    userID,
		"trigger":    trigger,
		"started_at": bson.M{"$gte": since},
	})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (r *CycleRepo) AddDraft(ctx context.Context, cycleID string, draftID string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": cycleID}, bson.M{
		"$push": bson.M{"draft_ids": draftID},
	})
	return err
}
