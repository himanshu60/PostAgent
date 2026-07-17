// Command regen regenerates a single draft's content + image using the current
// generator prompts, then writes the new body/hashtags/media back to that draft.
// Usage: go run ./cmd/regen <draftID>
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: go run ./cmd/regen <draftID>")
	}
	draftID := os.Args[1]

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	mdb, err := storage.NewMongoDB(ctx, cfg.MongoDB, logger)
	if err != nil {
		log.Fatalf("mongo: %v", err)
	}
	defer mdb.Close(ctx)

	drafts := storage.NewDraftRepo(mdb)
	topics := storage.NewTopicRepo(mdb)

	draft, err := drafts.GetByID(ctx, draftID)
	if err != nil {
		log.Fatalf("get draft: %v", err)
	}
	topic, err := topics.GetByID(ctx, draft.TopicID)
	if err != nil {
		log.Fatalf("get topic: %v", err)
	}

	llmClient, err := llm.NewFromConfig(cfg, logger)
	if err != nil {
		log.Fatalf("llm: %v", err)
	}
	gen := generator.NewLLMGenerator(llmClient, logger)
	voice := generator.BrandVoiceFromConfig(cfg)

	fmt.Printf("Regenerating draft %s (topic: %s)...\n", draftID, topic.Title)
	newDraft, err := gen.GenerateFromTopic(ctx, *topic, voice, "")
	if err != nil {
		log.Fatalf("generate: %v", err)
	}

	if err := drafts.UpdateBody(ctx, draftID, newDraft.Body); err != nil {
		log.Fatalf("update body: %v", err)
	}
	if err := drafts.UpdateHashtags(ctx, draftID, newDraft.Hashtags); err != nil {
		log.Fatalf("update hashtags: %v", err)
	}
	if err := drafts.UpdateMedia(ctx, draftID, newDraft.Media); err != nil {
		log.Fatalf("update media: %v", err)
	}

	fmt.Printf("\n=== NEW BODY ===\n%s\n\n=== HASHTAGS ===\n%v\n\n=== MEDIA ===\n%s\n",
		newDraft.Body, newDraft.Hashtags, newDraft.Media.SourceURL)
	fmt.Println("Done.")
}
