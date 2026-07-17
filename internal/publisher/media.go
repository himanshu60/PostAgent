package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type mediaUploadResponse struct {
	Value struct {
		UploadURL string `json:"uploadUrl"`
		// The versioned images/documents APIs return the URN in "image"/"document".
		// "asset" is kept as a fallback for the legacy assets API.
		Image    string `json:"image"`
		Document string `json:"document"`
		Asset    string `json:"asset"`
	} `json:"value"`
}

// mediaURN returns whichever URN field the API populated.
func (r mediaUploadResponse) mediaURN() string {
	switch {
	case r.Value.Image != "":
		return r.Value.Image
	case r.Value.Document != "":
		return r.Value.Document
	default:
		return r.Value.Asset
	}
}

// UploadImageFromURL downloads a remote image to a temp file and uploads it to
// LinkedIn, returning the image URN to attach to a post.
func (c *Client) UploadImageFromURL(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return "", err
	}
	// Image-generation URLs (e.g. Pollinations) render on demand and can be slow.
	httpClient := &http.Client{Timeout: 90 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download image failed (status %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 6*1024*1024))
	if err != nil {
		return "", fmt.Errorf("read image: %w", err)
	}

	ext := ".jpg"
	if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "png") {
		ext = ".png"
	}
	tmp, err := os.CreateTemp("", "li_media_*"+ext)
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

func (c *Client) UploadImage(ctx context.Context, filePath string) (assetURN string, err error) {
	token, err := c.oauth.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}

	registerURL := "https://api.linkedin.com/rest/images?action=initializeUpload"
	registerBody := map[string]interface{}{
		"initializeUploadRequest": map[string]string{
			"owner": "urn:li:person:" + c.personID,
		},
	}

	body, _ := json.Marshal(registerBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("LinkedIn-Version", c.apiVersion)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("register upload: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register upload failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var uploadResp mediaUploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("parse upload response: %w", err)
	}

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	ext := filepath.Ext(filePath)
	contentType := "image/jpeg"
	if ext == ".png" {
		contentType = "image/png"
	}

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPut,
		uploadResp.Value.UploadURL, bytes.NewReader(fileData))
	if err != nil {
		return "", err
	}
	uploadReq.Header.Set("Content-Type", contentType)

	uploadRespHTTP, err := httpClient.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}
	uploadRespHTTP.Body.Close()

	if uploadRespHTTP.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("file upload failed (status %d)", uploadRespHTTP.StatusCode)
	}

	return uploadResp.mediaURN(), nil
}

func (c *Client) UploadPDF(ctx context.Context, filePath string) (assetURN string, err error) {
	token, err := c.oauth.GetToken(ctx)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}

	registerURL := "https://api.linkedin.com/rest/documents?action=initializeUpload"
	registerBody := map[string]interface{}{
		"initializeUploadRequest": map[string]string{
			"owner": "urn:li:person:" + c.personID,
		},
	}

	body, _ := json.Marshal(registerBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, registerURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("LinkedIn-Version", c.apiVersion)
	req.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("register pdf upload: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("register pdf upload failed (status %d): %s", resp.StatusCode, string(respBody))
	}

	var uploadResp mediaUploadResponse
	if err := json.Unmarshal(respBody, &uploadResp); err != nil {
		return "", fmt.Errorf("parse pdf upload response: %w", err)
	}

	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read pdf: %w", err)
	}

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPut,
		uploadResp.Value.UploadURL, bytes.NewReader(fileData))
	if err != nil {
		return "", err
	}
	uploadReq.Header.Set("Content-Type", "application/pdf")

	uploadRespHTTP, err := httpClient.Do(uploadReq)
	if err != nil {
		return "", fmt.Errorf("upload pdf: %w", err)
	}
	uploadRespHTTP.Body.Close()

	if uploadRespHTTP.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("pdf upload failed (status %d)", uploadRespHTTP.StatusCode)
	}

	return uploadResp.mediaURN(), nil
}
