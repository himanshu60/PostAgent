package storage

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

type UserRepo struct {
	coll *mongo.Collection
}

func NewUserRepo(db *MongoDB) *UserRepo {
	return &UserRepo{coll: db.Users()}
}

// Upsert creates or updates a user, keyed by their LinkedIn id (u.ID). Identity
// fields and tokens are always refreshed on login; per-user settings are only
// set on first creation so they aren't clobbered.
func (r *UserRepo) Upsert(ctx context.Context, u *models.User) error {
	now := time.Now()
	set := bson.M{
		"name":             u.Name,
		"email":            u.Email,
		"picture":          u.Picture,
		"access_token":     u.AccessToken,
		"refresh_token":    u.RefreshToken,
		"token_expires_at": u.TokenExpiresAt,
		"last_login_at":    now,
	}
	setOnInsert := bson.M{
		"created_at":   now,
		"auto_publish": false,
		"publish_days": u.PublishDays,
	}
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": u.ID},
		bson.M{"$set": set, "$setOnInsert": setOnInsert},
		options.Update().SetUpsert(true),
	)
	return err
}

func (r *UserRepo) GetByID(ctx context.Context, id string) (*models.User, error) {
	var u models.User
	if err := r.coll.FindOne(ctx, bson.M{"_id": id}).Decode(&u); err != nil {
		return nil, err
	}
	return &u, nil
}

// UpdateTokens persists refreshed LinkedIn tokens for a user.
func (r *UserRepo) UpdateTokens(ctx context.Context, id, access, refresh string, expiresAt time.Time) error {
	set := bson.M{"access_token": access, "token_expires_at": expiresAt}
	if refresh != "" {
		set["refresh_token"] = refresh
	}
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	return err
}

// UpdateSettings persists a user's posting preferences.
func (r *UserRepo) UpdateSettings(ctx context.Context, id string, autoPublish bool, publishDays string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"auto_publish": autoPublish, "publish_days": publishDays},
	})
	return err
}

// UpdatePreferences persists a user's designation (role) and interest topics,
// which tailor the posts the agent generates for them.
func (r *UserRepo) UpdatePreferences(ctx context.Context, id, role string, interests []string) error {
	_, err := r.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{"role": role, "interests": interests},
	})
	return err
}

// Count returns how many users have ever signed in (total tenants).
func (r *UserRepo) Count(ctx context.Context) int64 {
	n, err := r.coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0
	}
	return n
}

// List returns all users — used by the scheduler to run each tenant's cycle.
func (r *UserRepo) List(ctx context.Context) ([]models.User, error) {
	cursor, err := r.coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var users []models.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	return users, nil
}

type SessionRepo struct {
	coll *mongo.Collection
}

func NewSessionRepo(db *MongoDB) *SessionRepo {
	return &SessionRepo{coll: db.Sessions()}
}

func (r *SessionRepo) Create(ctx context.Context, s *models.Session) error {
	_, err := r.coll.InsertOne(ctx, s)
	return err
}

// Get returns the session for a token if it exists and hasn't expired.
func (r *SessionRepo) Get(ctx context.Context, token string) (*models.Session, error) {
	var s models.Session
	if err := r.coll.FindOne(ctx, bson.M{"_id": token}).Decode(&s); err != nil {
		return nil, err
	}
	if time.Now().After(s.ExpiresAt) {
		_ = r.Delete(ctx, token)
		return nil, mongo.ErrNoDocuments
	}
	return &s, nil
}

func (r *SessionRepo) Delete(ctx context.Context, token string) error {
	_, err := r.coll.DeleteOne(ctx, bson.M{"_id": token})
	return err
}
