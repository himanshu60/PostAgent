package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrDuplicatePost is returned when LinkedIn rejects a post because identical
// content already exists — meaning it was, in fact, already published.
var ErrDuplicatePost = errors.New("linkedin: duplicate post (already published)")

type linkedInPostRequest struct {
	Author         string       `json:"author"`
	Commentary     string       `json:"commentary"`
	Visibility     string       `json:"visibility"`
	LifecycleState string       `json:"lifecycleState"`
	Distribution   distribution `json:"distribution"`
	Content        *postContent `json:"content,omitempty"`
}

type distribution struct {
	FeedDistribution string `json:"feedDistribution"`
}

type postContent struct {
	Media *mediaContent `json:"media,omitempty"`
}

type mediaContent struct {
	ID      string `json:"id"`
	AltText string `json:"altText,omitempty"`
}

type linkedInPostResponse struct {
	ID string `json:"id"`
}

func (c *Client) postToLinkedIn(ctx context.Context, accessToken string, req linkedInPostRequest) (*linkedInPostResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal post request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.linkedin.com/rest/posts", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("LinkedIn-Version", c.apiVersion)
	httpReq.Header.Set("X-Restli-Protocol-Version", "2.0.0")
	httpReq.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http post: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnprocessableEntity && strings.Contains(string(respBody), "DUPLICATE_POST") {
		return nil, ErrDuplicatePost
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("linkedin API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// The Posts API returns 201 with an EMPTY body; the new post URN is in the
	// x-restli-id response header. Prefer that, and only fall back to the body.
	if id := resp.Header.Get("x-restli-id"); id != "" {
		return &linkedInPostResponse{ID: id}, nil
	}
	if id := resp.Header.Get("x-linkedin-id"); id != "" {
		return &linkedInPostResponse{ID: id}, nil
	}

	if len(respBody) > 0 {
		var postResp linkedInPostResponse
		if err := json.Unmarshal(respBody, &postResp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		if postResp.ID != "" {
			return &postResp, nil
		}
	}

	// 201 with no id anywhere: the post was created, we just can't report its URN.
	c.logger.Warn("LinkedIn post created but no id returned in header or body")
	return &linkedInPostResponse{}, nil
}

func (c *Client) FetchAnalytics(ctx context.Context, linkedInPostID string) (likes, comments, impressions int, err error) {
	accessToken, err := c.oauth.GetToken(ctx)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("get token: %w", err)
	}

	url := fmt.Sprintf("https://api.linkedin.com/rest/posts/%s", linkedInPostID)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("LinkedIn-Version", c.apiVersion)
	httpReq.Header.Set("X-Restli-Protocol-Version", "2.0.0")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return 0, 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, 0, 0, fmt.Errorf("analytics API error (status %d)", resp.StatusCode)
	}

	return 0, 0, 0, nil
}
