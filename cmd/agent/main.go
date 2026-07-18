package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/agent"
	"github.com/clodura/linkedin-post-agent/internal/analyzer"
	"github.com/clodura/linkedin-post-agent/internal/config"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/publisher"
	"github.com/clodura/linkedin-post-agent/internal/research"
	"github.com/clodura/linkedin-post-agent/internal/scheduler"
	"github.com/clodura/linkedin-post-agent/internal/storage"
	webui "github.com/clodura/linkedin-post-agent/internal/ui/web"
)

func main() {
	serve := flag.Bool("serve", false, "Start web UI server")
	// Default the listen address to $PORT when set (Render, Railway, Koyeb, Fly,
	// Cloud Run all inject it), falling back to :8081 for local dev. An explicit
	// -addr flag still overrides both.
	defaultAddr := ":8081"
	if p := os.Getenv("PORT"); p != "" {
		defaultAddr = ":" + p
	}
	addr := flag.String("addr", defaultAddr, "Web UI server address (defaults to :$PORT, else :8081)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	mdb, err := storage.NewMongoDB(ctx, cfg.MongoDB, logger)
	if err != nil {
		logger.Fatal("Failed to connect to MongoDB", zap.Error(err))
	}
	defer mdb.Close(ctx)

	llmClient, err := llm.NewFromConfig(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to create LLM client", zap.Error(err))
	}
	logger.Info("LLM client created",
		zap.String("provider", llmClient.Provider()),
		zap.String("llm_model", cfg.LLM.Model),
		zap.String("ollama_model", cfg.Ollama.Model),
	)

	draftRepo := storage.NewDraftRepo(mdb)
	topicRepo := storage.NewTopicRepo(mdb)
	publishedRepo := storage.NewPublishedRepo(mdb)
	cycleRepo := storage.NewCycleRepo(mdb)
	userRepo := storage.NewUserRepo(mdb)
	sessionRepo := storage.NewSessionRepo(mdb)
	mediaRepo := storage.NewMediaRepo(mdb)

	rssFetcher := research.NewRSSFetcher(nil, logger)
	newsFetcher := research.NewNewsAPIFetcher(cfg.NewsAPI.APIKey, nil, logger)
	researchAgent := research.NewAgent(rssFetcher, newsFetcher, logger)

	gen := generator.NewLLMGenerator(llmClient, logger)
	voice := generator.BrandVoiceFromConfig(cfg)

	imageGen := publisher.ImageGenConfig{
		Provider:    cfg.Image.Provider,
		OpenAIKey:   cfg.Image.OpenAIKey,
		OpenAIModel: cfg.Image.OpenAIModel,
		GeminiKey:   cfg.Gemini.APIKey,
		GeminiModel: cfg.Image.GeminiModel,
	}

	// newPublisher builds a LinkedIn client authored by a specific user, using
	// that user's own OAuth tokens — so every post goes to the poster's account.
	newPublisher := func(u *models.User) *publisher.Client {
		c := publisher.NewClient(config.LinkedInConfig{
			ClientID:     cfg.LinkedIn.ClientID,
			ClientSecret: cfg.LinkedIn.ClientSecret,
			RedirectURI:  cfg.LinkedIn.RedirectURI,
			APIVersion:   cfg.LinkedIn.APIVersion,
			AccessToken:  u.AccessToken,
			RefreshToken: u.RefreshToken,
			PersonID:     u.ID,
		}, logger)
		c.SetImageGen(imageGen)
		return c
	}

	exampleStore := analyzer.NewExampleStore(mdb.DB(), logger)

	agt := agent.New(
		researchAgent,
		gen,
		draftRepo,
		topicRepo,
		publishedRepo,
		cycleRepo,
		userRepo,
		voice,
		exampleStore,
		newPublisher,
		logger,
	)

	sched := scheduler.New(agt, logger)

	if *serve {
		webServer := webui.NewServer(*addr, logger, agt, exampleStore, draftRepo, topicRepo, publishedRepo, cycleRepo, userRepo, sessionRepo, mediaRepo, cfg.LinkedIn, gen, voice, cfg.Agent.TopicsCount)
		if err := webServer.Start(); err != nil {
			logger.Fatal("Failed to start web server", zap.Error(err))
		}

		logger.Info("Web UI available", zap.String("addr", *addr))
		fmt.Printf("\n  Open http://localhost%s in your browser\n\n", *addr)

		if err := sched.Start(ctx); err != nil {
			logger.Fatal("Failed to start scheduler", zap.Error(err))
		}

		<-sigCh
		logger.Info("Shutting down...")
		sched.Stop()
		return
	}

	// Headless: run only the publish poller (publishes each user's scheduled
	// posts with their own tokens).
	if err := sched.Start(ctx); err != nil {
		logger.Fatal("Failed to start scheduler", zap.Error(err))
	}

	<-sigCh
	logger.Info("Shutting down...")
	sched.Stop()
}
