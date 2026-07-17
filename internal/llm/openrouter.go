package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/clodura/linkedin-post-agent/internal/config"
)

type OpenRouterClient struct {
	apiKey  string
	baseURL string
	model   string
	http    *http.Client
}

func NewOpenRouterClient(cfg config.OpenRouterConfig, model string) *OpenRouterClient {
	return &OpenRouterClient{
		apiKey:  cfg.APIKey,
		baseURL: cfg.BaseURL,
		model:   model,
		http: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type openRouterRequest struct {
	Model    string             `json:"model"`
	Messages []openRouterMsg    `json:"messages"`
}

type openRouterMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *OpenRouterClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	orReq := openRouterRequest{
		Model: c.model,
	}
	for _, m := range req.Messages {
		orReq.Messages = append(orReq.Messages, openRouterMsg{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(orReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("HTTP-Referer", "https://github.com/clodura/linkedin-post-agent")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBody, &orResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if orResp.Error != nil {
		return nil, fmt.Errorf("openrouter error: %s", orResp.Error.Message)
	}

	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	return &ChatResponse{
		Content: orResp.Choices[0].Message.Content,
		Model:   orResp.Model,
	}, nil
}

func (c *OpenRouterClient) Provider() string {
	return "openrouter"
}
