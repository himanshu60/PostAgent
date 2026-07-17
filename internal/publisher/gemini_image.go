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

// generateGeminiImage calls the Gemini image API ("Nano Banana" models) and
// returns raw image bytes. Gemini renders readable text well, so the stored
// cover prompt (title as typography) works directly.
func generateGeminiImage(ctx context.Context, cfg ImageGenConfig, prompt string) ([]byte, error) {
	model := cfg.GeminiModel
	if model == "" {
		model = "gemini-3.1-flash-lite-image"
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"contents": []map[string]interface{}{
			{"parts": []map[string]string{{"text": prompt}}},
		},
		"generationConfig": map[string]interface{}{
			"responseModalities": []string{"TEXT", "IMAGE"},
		},
	})

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent", model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-goog-api-key", cfg.GeminiKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini image request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini image error (status %d): %s", resp.StatusCode, truncateStr(string(body), 300))
	}

	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					InlineData *struct {
						MimeType string `json:"mimeType"`
						Data     string `json:"data"`
					} `json:"inlineData"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("parse gemini response: %w", err)
	}
	for _, c := range out.Candidates {
		for _, p := range c.Content.Parts {
			if p.InlineData != nil && p.InlineData.Data != "" {
				return base64.StdEncoding.DecodeString(p.InlineData.Data)
			}
		}
	}
	return nil, fmt.Errorf("gemini returned no image data")
}

// uploadGeminiImage generates an image with Gemini, writes it to a temp file,
// and uploads it to LinkedIn, returning the image URN.
func (c *Client) uploadGeminiImage(ctx context.Context, prompt string) (string, error) {
	data, err := generateGeminiImage(ctx, c.imageGen, prompt)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp("", "gem_img_*.png")
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

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
