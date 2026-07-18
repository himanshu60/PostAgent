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

type DraftRepo struct {
	coll *mongo.Collection
}

func NewDraftRepo(db *MongoDB) *DraftRepo {
	return &DraftRepo{coll: db.Drafts()}
}

func (r *DraftRepo) Insert(ctx context.Context, draft *models.Draft) error {
	if draft.ID == "" {
		draft.ID = uuid.New().String()
	}
	now := time.Now()
	draft.CreatedAt = now
	draft.UpdatedAt = now
	_, err := r.coll.InsertOne(ctx, draft)
	return err
}

func (r *DraftRepo) InsertMany(ctx context.Context, drafts []models.Draft) error {
	if len(drafts) == 0 {
		return nil
	}
	now := time.Now()
	for i := range drafts {
		if drafts[i].ID == "" {
			drafts[i].ID = uuid.New().String()
		}
		drafts[i].CreatedAt = now
		drafts[i].UpdatedAt = now
	}
	docs := make([]interface{}, len(drafts))
	for i := range drafts {
		docs[i] = drafts[i]
	}
	_, err := r.coll.InsertMany(ctx, docs)
	return err
}

func (r *DraftRepo) GetByID(ctx context.Context, id string) (*models.Draft, error) {
	var draft models.Draft
	err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&draft)
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (r *DraftRepo) GetByCycleID(ctx context.Context, cycleID string) ([]models.Draft, error) {
	cursor, err := r.coll.Find(ctx, bson.M{"cycle_id": cycleID})
	if err != nil {
		return nil, err
	}
	var drafts []models.Draft
	if err := cursor.All(ctx, &drafts); err != nil {
		return nil, err
	}
	return drafts, nil
}

func (r *DraftRepo) GetNextPending(ctx context.Context) (*models.Draft, error) {
	filter := bson.M{
		"status":        bson.M{"$in": []models.DraftStatus{models.DraftStatusApproved}},
		"scheduled_for": bson.M{"$lte": time.Now()},
	}
	var draft models.Draft
	err := r.coll.FindOne(ctx, filter).Decode(&draft)
	if err != nil {
		return nil, err
	}
	return &draft, nil
}

func (r *DraftRepo) UpdateStatus(ctx context.Context, id string, status models.DraftStatus) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"status": status, "updated_at": time.Now()},
	})
	return err
}

func (r *DraftRepo) UpdateMedia(ctx context.Context, id string, media *models.Media) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"media": media, "updated_at": time.Now()},
	})
	return err
}

// CountScheduledBetween returns how many approved drafts (other than
// excludeID) are scheduled in [start, end). Used to enforce at most one
// scheduled post per calendar day: both manual (re)scheduling and the weekly
// auto-generator check the target day and move to the next free one.
func (r *DraftRepo) CountScheduledBetween(ctx context.Context, excludeID string, start, end time.Time) (int64, error) {
	filter := bson.M{
		"_id":           bson.M{"$ne": excludeID},
		"status":        models.DraftStatusApproved,
		"scheduled_for": bson.M{"$gte": start, "$lt": end},
	}
	return r.coll.CountDocuments(ctx, filter)
}

func (r *DraftRepo) UpdateScheduledFor(ctx context.Context, id string, t time.Time) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"scheduled_for": t, "updated_at": time.Now()},
	})
	return err
}

func (r *DraftRepo) ClearScheduledFor(ctx context.Context, id string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"scheduled_for": nil, "updated_at": time.Now()},
	})
	return err
}

func (r *DraftRepo) UpdateBody(ctx context.Context, id string, body string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"body": body, "updated_at": time.Now()},
	})
	return err
}

func (r *DraftRepo) UpdateHashtags(ctx context.Context, id string, hashtags []string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"hashtags": hashtags, "updated_at": time.Now()},
	})
	return err
}

func (r *DraftRepo) List(ctx context.Context, userID, status string, limit int) ([]models.Draft, error) {
	filter := bson.M{"user_id": userID}
	if status != "" {
		filter["status"] = status
	}
	opts := options.Find().SetSort(bson.M{"created_at": -1}).SetLimit(int64(limit))
	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var drafts []models.Draft
	if err := cursor.All(ctx, &drafts); err != nil {
		return nil, err
	}
	return drafts, nil
}
