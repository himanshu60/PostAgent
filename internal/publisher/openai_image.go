package publisher

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ImageGenConfig configures optional live image generation at publish time.
type ImageGenConfig struct {
	Provider    string // "gemini" or "openai"; anything else uses the fallback image URL
	OpenAIKey   string
	OpenAIModel string
	GeminiKey   string
	GeminiModel string
}

func (c ImageGenConfig) openAIEnabled() bool {
	return c.Provider == "openai" && c.OpenAIKey != ""
}

func (c ImageGenConfig) geminiEnabled() bool {
	return c.Provider == "gemini" && c.GeminiKey != ""
}

// generateOpenAIImage calls the OpenAI Images API and returns the raw PNG bytes.
func generateOpenAIImage(ctx context.Context, cfg ImageGenConfig, prompt string) ([]byte, error) {
	model := cfg.OpenAIModel
	if model == "" {
		model = "gpt-image-1"
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"n":      1,
		"size":   "1024x1024",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/images/generations", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.OpenAIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai image request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai image error (status %d): %s", resp.StatusCode, string(body))
	}

	var out struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("openai returned no image")
	}

	if b64 := out.Data[0].B64JSON; b64 != "" {
		return base64.StdEncoding.DecodeString(b64)
	}
	if u := out.Data[0].URL; u != "" {
		return downloadBytes(ctx, u)
	}
	return nil, fmt.Errorf("openai response had no image data")
}

func downloadBytes(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed (status %d)", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 12*1024*1024))
}

// uploadOpenAIImage generates an image with OpenAI, writes it to a temp file,
// and uploads it to LinkedIn, returning the image URN.
func (c *Client) uploadOpenAIImage(ctx context.Context, prompt string) (string, error) {
	data, err := generateOpenAIImage(ctx, c.imageGen, prompt)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "oai_img_*.png")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	return c.UploadImage(ctx, tmpPath)
}
