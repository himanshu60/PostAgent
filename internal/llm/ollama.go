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

type OllamaClient struct {
	baseURL string
	model   string
	http    *http.Client
}

func NewOllamaClient(cfg config.OllamaConfig) *OllamaClient {
	return &OllamaClient{
		baseURL: cfg.BaseURL,
		model:   cfg.Model,
		http: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

type ollamaRequest struct {
	Model    string      `json:"model"`
	Messages []ollamaMsg `json:"messages"`
	Stream   bool        `json:"stream"`
}

type ollamaMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaResponse struct {
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Model string `json:"model"`
	Error string `json:"error,omitempty"`
}

func (c *OllamaClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	oReq := ollamaRequest{
		Model:  c.model,
		Stream: false,
	}
	for _, m := range req.Messages {
		oReq.Messages = append(oReq.Messages, ollamaMsg{Role: m.Role, Content: m.Content})
	}

	body, err := json.Marshal(oReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

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
		return nil, fmt.Errorf("ollama API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var oResp ollamaResponse
	if err := json.Unmarshal(respBody, &oResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if oResp.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", oResp.Error)
	}

	return &ChatResponse{
		Content: oResp.Message.Content,
		Model:   oResp.Model,
	}, nil
}

func (c *OllamaClient) Provider() string {
	return "ollama"
}
