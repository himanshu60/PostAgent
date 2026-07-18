package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	LLM        LLMConfig
	OpenRouter OpenRouterConfig
	Ollama     OllamaConfig
	Gemini     GeminiConfig
	MongoDB    MongoDBConfig
	LinkedIn   LinkedInConfig
	Image      ImageConfig
	Agent      AgentConfig
	BrandVoice BrandVoiceConfig
	NewsAPI    NewsAPIConfig
	Logging    LoggingConfig
}

type LLMConfig struct {
	Provider         string // openrouter | ollama | gemini
	Model            string
	FallbackProvider string // optional: openrouter | ollama | gemini — used when Provider fails/hits quota
}

type OpenRouterConfig struct {
	APIKey  string
	BaseURL string
}

type GeminiConfig struct {
	APIKey string
	Model  string
}

type ImageConfig struct {
	Provider    string // pollinations | openai | gemini
	OpenAIKey   string
	OpenAIModel string
	GeminiModel string
}

type OllamaConfig struct {
	BaseURL string
	Model   string
}

type MongoDBConfig struct {
	URI     string
	DB      string
	PoolMin int
	PoolMax int
	Timeout time.Duration
}

type LinkedInConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	PersonID     string
	AccessToken  string
	RefreshToken string
	APIVersion   string
}

type NewsAPIConfig struct {
	APIKey string
}

type AgentConfig struct {
	Schedule    string
	TopicsCount int
	AutoPublish bool
	// PublishDays are the weekdays auto-generated posts get scheduled for
	// (e.g. Mon/Wed/Fri = 3 posts a week). The generator runs the day before
	// each publish day.
	PublishDays []time.Weekday
}

// parseWeekdays turns "Mon,Wed,Fri" into weekdays; unknown names are skipped.
func parseWeekdays(s string) []time.Weekday {
	names := map[string]time.Weekday{
		"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
		"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday,
		"sat": time.Saturday,
	}
	var days []time.Weekday
	for _, part := range strings.Split(s, ",") {
		key := strings.ToLower(strings.TrimSpace(part))
		if len(key) > 3 {
			key = key[:3]
		}
		if d, ok := names[key]; ok {
			days = append(days, d)
		}
	}
	return days
}

type BrandVoiceConfig struct {
	Tone          string // professional | casual | thought-leadership
	MaxEmoji      int
	MinLineBreaks int
}

type LoggingConfig struct {
	Level string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		LLM: LLMConfig{
			Provider:         getEnv("LLM_PROVIDER", "openrouter"),
			Model:            getEnv("LLM_MODEL", "meta-llama/llama-3-70b-instruct"),
			FallbackProvider: getEnv("LLM_FALLBACK_PROVIDER", ""),
		},
		OpenRouter: OpenRouterConfig{
			APIKey:  os.Getenv("OPENROUTER_API_KEY"),
			BaseURL: getEnv("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"),
		},
		Ollama: OllamaConfig{
			BaseURL: getEnv("OLLAMA_BASE_URL", "http://localhost:11434"),
			Model:   getEnv("OLLAMA_MODEL", "llama3:8b"),
		},
		Gemini: GeminiConfig{
			APIKey: os.Getenv("GEMINI_API_KEY"),
			Model:  getEnv("GEMINI_MODEL", "gemini-2.5-flash"),
		},
		MongoDB: MongoDBConfig{
			URI:     os.Getenv("MONGO_URI"),
			DB:      getEnv("MONGO_DB", "linkedin_agent"),
			PoolMin: getEnvInt("MONGO_POOL_MIN", 1),
			PoolMax: getEnvInt("MONGO_POOL_MAX", 10),
			Timeout: getEnvDuration("MONGO_TIMEOUT", 5*time.Second),
		},
		LinkedIn: LinkedInConfig{
			ClientID:     os.Getenv("LINKEDIN_CLIENT_ID"),
			ClientSecret: os.Getenv("LINKEDIN_CLIENT_SECRET"),
			RedirectURI:  getEnv("LINKEDIN_REDIRECT_URI", "http://localhost:8080/auth/callback"),
			PersonID:     os.Getenv("LINKEDIN_PERSON_ID"),
			AccessToken:  os.Getenv("LINKEDIN_ACCESS_TOKEN"),
			RefreshToken: os.Getenv("LINKEDIN_REFRESH_TOKEN"),
			APIVersion:   getEnv("LINKEDIN_API_VERSION", "202606"),
		},
		Image: ImageConfig{
			Provider:    getEnv("IMAGE_PROVIDER", "pollinations"),
			OpenAIKey:   os.Getenv("OPENAI_API_KEY"),
			OpenAIModel: getEnv("OPENAI_IMAGE_MODEL", "gpt-image-1"),
			GeminiModel: getEnv("GEMINI_IMAGE_MODEL", "gemini-3.1-flash-lite-image"),
		},
		NewsAPI: NewsAPIConfig{
			APIKey: os.Getenv("NEWSAPI_KEY"),
		},
		Agent: AgentConfig{
			Schedule:    getEnv("AGENT_SCHEDULE", "0 9 * * 1,3,5"),
			TopicsCount: getEnvInt("AGENT_TOPICS_COUNT", 5),
			AutoPublish: getEnvBool("AGENT_AUTO_PUBLISH", false),
			PublishDays: parseWeekdays(getEnv("AGENT_PUBLISH_DAYS", "Mon,Wed,Fri")),
		},
		BrandVoice: BrandVoiceConfig{
			Tone:          getEnv("BRAND_VOICE_TONE", "professional"),
			MaxEmoji:      getEnvInt("BRAND_VOICE_MAX_EMOJI", 3),
			MinLineBreaks: getEnvInt("BRAND_VOICE_MIN_LINE_BREAKS", 3),
		},
		Logging: LoggingConfig{
			Level: getEnv("LOG_LEVEL", "info"),
		},
	}

	if cfg.OpenRouter.APIKey == "" && cfg.LLM.Provider == "openrouter" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY is required when LLM_PROVIDER=openrouter")
	}
	if cfg.MongoDB.URI == "" {
		return nil, fmt.Errorf("MONGO_URI is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if val := os.Getenv(key); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			return n
		}
	}
	return fallback
}

func (c *Config) GetTone() string       { return c.BrandVoice.Tone }
func (c *Config) GetMaxEmoji() int      { return c.BrandVoice.MaxEmoji }
func (c *Config) GetMinLineBreaks() int { return c.BrandVoice.MinLineBreaks }

func getEnvBool(key string, fallback bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return fallback
}
