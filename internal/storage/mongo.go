package storage

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
)

type MongoDB struct {
	client *mongo.Client
	db     *mongo.Database
	logger *zap.Logger
}

func NewMongoDB(ctx context.Context, cfg config.MongoDBConfig, logger *zap.Logger) (*MongoDB, error) {
	opts := options.Client().
		ApplyURI(cfg.URI).
		SetMinPoolSize(uint64(cfg.PoolMin)).
		SetMaxPoolSize(uint64(cfg.PoolMax)).
		SetConnectTimeout(cfg.Timeout)

	client, err := mongo.Connect(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}

	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	db := client.Database(cfg.DB)
	logger.Info("Connected to MongoDB", zap.String("db", cfg.DB))

	return &MongoDB{
		client: client,
		db:     db,
		logger: logger,
	}, nil
}

func (m *MongoDB) Close(ctx context.Context) error {
	return m.client.Disconnect(ctx)
}

func (m *MongoDB) DB() *mongo.Database {
	return m.db
}

func (m *MongoDB) Collection(name string) *mongo.Collection {
	return m.db.Collection(name)
}

func (m *MongoDB) Topics() *mongo.Collection {
	return m.db.Collection("agent_topics")
}

func (m *MongoDB) Drafts() *mongo.Collection {
	return m.db.Collection("agent_drafts")
}

func (m *MongoDB) Published() *mongo.Collection {
	return m.db.Collection("agent_published")
}

func (m *MongoDB) Cycles() *mongo.Collection {
	return m.db.Collection("agent_cycles")
}

func (m *MongoDB) Users() *mongo.Collection {
	return m.db.Collection("agent_users")
}

func (m *MongoDB) Sessions() *mongo.Collection {
	return m.db.Collection("agent_sessions")
}

func (m *MongoDB) Media() *mongo.Collection {
	return m.db.Collection("agent_media")
}
