package llm

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
}

type ChatResponse struct {
	Content string
	Model   string
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Provider() string
}
