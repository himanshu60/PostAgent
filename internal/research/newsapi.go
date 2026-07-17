package research

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.uber.org/zap"
)

// defaultNewsKeywords target software-engineering subjects specifically.
// Broad terms like "technology"/"startup"/"B2B"/"sales" were removed — they
// pulled in consumer and business news unrelated to a developer audience.
var defaultNewsKeywords = []string{
	"software engineering", "programming languages", "developer tools",
	"DevOps", "cloud computing", "system design",
	"artificial intelligence coding", "open source software",
}

type NewsAPIFetcher struct {
	apiKey   string
	keywords []string
	http     *http.Client
	logger   *zap.Logger
}

func (n *NewsAPIFetcher) APIKey() string {
	if n == nil {
		return ""
	}
	return n.apiKey
}

func NewNewsAPIFetcher(apiKey string, keywords []string, logger *zap.Logger) *NewsAPIFetcher {
	if len(keywords) == 0 {
		keywords = defaultNewsKeywords
	}
	return &NewsAPIFetcher{
		apiKey:   apiKey,
		keywords: keywords,
		http:     &http.Client{Timeout: 15 * time.Second},
		logger:   logger,
	}
}

type newsAPIResponse struct {
	Status       string `json:"status"`
	TotalResults int    `json:"totalResults"`
	Articles     []struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		URL         string `json:"url"`
		URLToImage  string `json:"urlToImage"`
		Source      struct {
			Name string `json:"name"`
		} `json:"source"`
	} `json:"articles"`
}

func (n *NewsAPIFetcher) Search(ctx context.Context, query string, limit int) ([]FetchedTopic, error) {
	if n.apiKey == "" {
		return nil, fmt.Errorf("NewsAPI key not configured")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("pageSize", fmt.Sprintf("%d", limit))
	params.Set("language", "en")
	params.Set("sortBy", "relevancy")

	reqURL := fmt.Sprintf("https://newsapi.org/v2/everything?%s", params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Api-Key", n.apiKey)

	resp, err := n.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	var newsResp newsAPIResponse
	if err := json.Unmarshal(body, &newsResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if newsResp.Status != "ok" {
		return nil, fmt.Errorf("NewsAPI error: %s", newsResp.Status)
	}

	var topics []FetchedTopic
	seen := make(map[string]bool)
	for _, article := range newsResp.Articles {
		if article.Title == "" || article.URL == "" || seen[article.URL] {
			continue
		}
		seen[article.URL] = true

		summary := article.Description
		if len(summary) > 300 {
			summary = summary[:300] + "..."
		}

		topics = append(topics, FetchedTopic{
			Title:    article.Title,
			URL:      article.URL,
			Summary:  summary,
			Keywords: []string{query},
			ImageURL: article.URLToImage,
			Source:   SourceNewsAPI,
		})
	}

	return topics, nil
}

func (n *NewsAPIFetcher) Fetch(ctx context.Context) ([]FetchedTopic, error) {
	if n.apiKey == "" {
		n.logger.Warn("NewsAPI key not configured, skipping")
		return nil, nil
	}

	var allTopics []FetchedTopic
	seen := make(map[string]bool)

	for _, keyword := range n.keywords {
		select {
		case <-ctx.Done():
			return allTopics, ctx.Err()
		default:
		}

		params := url.Values{}
		params.Set("q", keyword)
		params.Set("pageSize", "5")
		params.Set("language", "en")
		params.Set("sortBy", "publishedAt")

		reqURL := fmt.Sprintf("https://newsapi.org/v2/everything?%s", params.Encode())
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("X-Api-Key", n.apiKey)

		resp, err := n.http.Do(req)
		if err != nil {
			n.logger.Warn("NewsAPI request failed", zap.String("keyword", keyword), zap.Error(err))
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var newsResp newsAPIResponse
		if err := json.Unmarshal(body, &newsResp); err != nil {
			continue
		}

		if newsResp.Status != "ok" {
			continue
		}

		for _, article := range newsResp.Articles {
			if article.Title == "" || article.URL == "" || seen[article.URL] {
				continue
			}
			seen[article.URL] = true

			summary := article.Description
			if len(summary) > 300 {
				summary = summary[:300] + "..."
			}

			allTopics = append(allTopics, FetchedTopic{
				Title:    article.Title,
				URL:      article.URL,
				Summary:  summary,
				Keywords: []string{keyword},
				ImageURL: article.URLToImage,
				Source:   SourceNewsAPI,
			})
		}
	}

	n.logger.Info("Fetched from NewsAPI", zap.Int("topics", len(allTopics)))
	return allTopics, nil
}
