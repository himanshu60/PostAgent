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

type GeminiClient struct {
	apiKey string
	model  string
	http   *http.Client
}

func NewGeminiClient(cfg config.GeminiConfig) *GeminiClient {
	model := cfg.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &GeminiClient{
		apiKey: cfg.APIKey,
		model:  model,
		http:   &http.Client{Timeout: 120 * time.Second},
	}
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *GeminiClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	gReq := geminiRequest{}
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			// Gemini takes the system prompt as a dedicated field.
			gReq.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: m.Content}}}
		case "assistant":
			gReq.Contents = append(gReq.Contents, geminiContent{Role: "model", Parts: []geminiPart{{Text: m.Content}}})
		default:
			gReq.Contents = append(gReq.Contents, geminiContent{Role: "user", Parts: []geminiPart{{Text: m.Content}}})
		}
	}

	body, err := json.Marshal(gReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", c.model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("x-goog-api-key", c.apiKey)
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
		return nil, fmt.Errorf("gemini API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var gResp geminiResponse
	if err := json.Unmarshal(respBody, &gResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if gResp.Error != nil {
		return nil, fmt.Errorf("gemini error: %s", gResp.Error.Message)
	}
	if len(gResp.Candidates) == 0 || len(gResp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini returned no content")
	}

	var text string
	for _, p := range gResp.Candidates[0].Content.Parts {
		text += p.Text
	}

	return &ChatResponse{Content: text, Model: c.model}, nil
}

func (c *GeminiClient) Provider() string {
	return "gemini"
}
