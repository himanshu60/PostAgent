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
	"github.com/clodura/linkedin-post-agent/internal/publisher"
	"github.com/clodura/linkedin-post-agent/internal/research"
	"github.com/clodura/linkedin-post-agent/internal/scheduler"
	"github.com/clodura/linkedin-post-agent/internal/storage"
	webui "github.com/clodura/linkedin-post-agent/internal/ui/web"
)

func main() {
	runOnce := flag.Bool("once", false, "Run one cycle and exit")
	auth := flag.Bool("auth", false, "Run LinkedIn OAuth2 flow")
	code := flag.String("code", "", "OAuth2 authorization code (used with -auth)")
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

	if *auth {
		if *code != "" {
			runAuthExchange(ctx, cfg, *code, logger)
		} else {
			showAuthURL(cfg, logger)
		}
		return
	}

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

	rssFetcher := research.NewRSSFetcher(nil, logger)
	newsFetcher := research.NewNewsAPIFetcher(cfg.NewsAPI.APIKey, nil, logger)
	researchAgent := research.NewAgent(rssFetcher, newsFetcher, logger)

	gen := generator.NewLLMGenerator(llmClient, logger)
	voice := generator.BrandVoiceFromConfig(cfg)

	var pub *publisher.Client
	if cfg.LinkedIn.ClientID != "" {
		pub = publisher.NewClient(cfg.LinkedIn, logger)
		pub.SetImageGen(publisher.ImageGenConfig{
			Provider:    cfg.Image.Provider,
			OpenAIKey:   cfg.Image.OpenAIKey,
			OpenAIModel: cfg.Image.OpenAIModel,
			GeminiKey:   cfg.Gemini.APIKey,
			GeminiModel: cfg.Image.GeminiModel,
		})
		if pub.IsConfigured() {
			logger.Info("LinkedIn publisher configured")
		} else {
			logger.Warn("LinkedIn publisher not fully configured, posts will be saved as drafts only")
		}
	} else {
		logger.Warn("LinkedIn credentials not set, posts will be saved as drafts only")
	}

	exampleStore := analyzer.NewExampleStore(mdb.DB(), logger)

	agt := agent.New(
		researchAgent,
		gen,
		draftRepo,
		topicRepo,
		publishedRepo,
		cycleRepo,
		pub,
		voice,
		exampleStore,
		cfg.Agent.AutoPublish,
		logger,
	)

	sched := scheduler.New(agt, cfg.Agent.Schedule, cfg.Agent.TopicsCount, cfg.Agent.PublishDays, logger)

	if *serve {
		webServer := webui.NewServer(*addr, logger, agt, exampleStore, draftRepo, topicRepo, publishedRepo, cycleRepo, gen, voice, cfg.Agent.TopicsCount)
		if err := webServer.Start(); err != nil {
			logger.Fatal("Failed to start web server", zap.Error(err))
		}

		logger.Info("Web UI available", zap.String("url", fmt.Sprintf("http://localhost%s", *addr)))
		fmt.Printf("\n  Open http://localhost%s in your browser\n\n", *addr)

		if err := sched.Start(ctx); err != nil {
			logger.Fatal("Failed to start scheduler", zap.Error(err))
		}

		<-sigCh
		logger.Info("Shutting down...")
		sched.Stop()
		return
	}

	if *runOnce {
		if err := sched.RunOnce(ctx); err != nil {
			logger.Fatal("RunOnce failed", zap.Error(err))
		}
		return
	}

	if err := sched.Start(ctx); err != nil {
		logger.Fatal("Failed to start scheduler", zap.Error(err))
	}

	<-sigCh
	logger.Info("Shutting down...")
	sched.Stop()
}

func showAuthURL(cfg *config.Config, logger *zap.Logger) {
	pub := publisher.NewClient(cfg.LinkedIn, logger)

	state := "linkedin-post-agent"
	authURL := pub.AuthURL(state)

	fmt.Println("\n─── LinkedIn OAuth2 Authorization ───")
	fmt.Println()
	fmt.Println("1. Open this URL in your browser:")
	fmt.Println()
	fmt.Println(authURL)
	fmt.Println()
	fmt.Println("2. Authorize the application")
	fmt.Println("3. You'll be redirected to your redirect URI with a ?code= parameter")
	fmt.Println("4. Copy the code and run:")
	fmt.Println()
	fmt.Printf("   %s -auth -code=YOUR_CODE\n", os.Args[0])
	fmt.Println()
}

func runAuthExchange(ctx context.Context, cfg *config.Config, authCode string, logger *zap.Logger) {
	pub := publisher.NewClient(cfg.LinkedIn, logger)

	if err := pub.ExchangeCode(ctx, authCode); err != nil {
		logger.Fatal("Failed to exchange code", zap.Error(err))
	}

	personID, err := pub.SetPersonID(ctx)
	if err != nil {
		logger.Fatal("Failed to get person ID", zap.Error(err))
	}

	fmt.Println("\n─── Authentication Successful ───")
	fmt.Println()
	fmt.Printf("LinkedIn Person ID: %s\n", personID)
	fmt.Println()
	fmt.Println("Add these to your .env file:")
	fmt.Printf("LINKEDIN_ACCESS_TOKEN=%s\n", pub.AccessToken())
	fmt.Printf("LINKEDIN_REFRESH_TOKEN=%s\n", pub.RefreshTokenStr())
	fmt.Printf("LINKEDIN_PERSON_ID=%s\n", personID)
	fmt.Println()
}
