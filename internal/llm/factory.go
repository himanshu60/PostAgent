package llm

import (
	"fmt"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/config"
)

func buildClient(provider string, cfg *config.Config) (Client, error) {
	switch provider {
	case "openrouter":
		return NewOpenRouterClient(cfg.OpenRouter, cfg.LLM.Model), nil
	case "ollama":
		return NewOllamaClient(cfg.Ollama), nil
	case "gemini":
		if cfg.Gemini.APIKey == "" {
			return nil, fmt.Errorf("gemini provider selected but GEMINI_API_KEY is empty")
		}
		return NewGeminiClient(cfg.Gemini), nil
	default:
		return nil, fmt.Errorf("unknown LLM provider: %s", provider)
	}
}

// NewFromConfig builds the configured LLM client using the given logger. If
// LLM_FALLBACK_PROVIDER is set, the returned client automatically retries
// against that fallback whenever the primary provider fails (quota
// exhaustion, rate limits, transient outages), instead of failing the whole
// operation.
func NewFromConfig(cfg *config.Config, logger *zap.Logger) (Client, error) {
	primary, err := buildClient(cfg.LLM.Provider, cfg)
	if err != nil {
		return nil, err
	}

	if cfg.LLM.FallbackProvider == "" || cfg.LLM.FallbackProvider == cfg.LLM.Provider {
		return primary, nil
	}

	fallback, err := buildClient(cfg.LLM.FallbackProvider, cfg)
	if err != nil {
		return nil, fmt.Errorf("fallback provider: %w", err)
	}

	return NewFallbackClient(primary, fallback, logger), nil
}
