package research

import (
	"context"
	"fmt"
	"time"

	"github.com/mmcdole/gofeed"
	"go.uber.org/zap"
)

// defaultRSSFeeds are developer-focused sources. Consumer-tech outlets (The
// Verge, Wired, TechCrunch) were removed: they surface gadget/business news
// (e.g. tractor right-to-repair settlements) that doesn't fit a software
// engineer's LinkedIn audience. (blog.google feed was also removed — 404.)
var defaultRSSFeeds = []string{
	"https://news.ycombinator.com/rss",
	"https://feed.infoq.com/",
	"https://thenewstack.io/feed/",
	"https://stackoverflow.blog/feed/",
	"https://github.blog/feed/",
	"https://dev.to/feed",
}

type RSSFetcher struct {
	feeds  []string
	fp     *gofeed.Parser
	logger *zap.Logger
}

func NewRSSFetcher(feeds []string, logger *zap.Logger) *RSSFetcher {
	if len(feeds) == 0 {
		feeds = defaultRSSFeeds
	}
	return &RSSFetcher{
		feeds:  feeds,
		fp:     gofeed.NewParser(),
		logger: logger,
	}
}

func (r *RSSFetcher) Fetch(ctx context.Context) ([]FetchedTopic, error) {
	var topics []FetchedTopic

	for _, feedURL := range r.feeds {
		select {
		case <-ctx.Done():
			return topics, ctx.Err()
		default:
		}

		feed, err := r.fp.ParseURLWithContext(feedURL, ctx)
		if err != nil {
			r.logger.Warn("Failed to parse RSS feed", zap.String("url", feedURL), zap.Error(err))
			continue
		}

		for _, item := range feed.Items {
			if item == nil || item.Title == "" || item.Link == "" {
				continue
			}

			summary := ""
			if item.Description != "" {
				summary = truncateText(stripHTML(item.Description), 300)
			}

			imageURL := ""
			if item.Image != nil {
				imageURL = item.Image.URL
			}

			published := time.Now()
			if item.PublishedParsed != nil {
				published = *item.PublishedParsed
			}

			_ = published

			topics = append(topics, FetchedTopic{
				Title:    item.Title,
				URL:      item.Link,
				Summary:  summary,
				Keywords: extractKeywords(item.Title, feed.Title),
				ImageURL: imageURL,
				Source:   SourceRSS,
			})
		}

		r.logger.Debug("Fetched RSS feed",
			zap.String("url", feedURL),
			zap.Int("items", len(topics)))
	}

	if len(topics) == 0 {
		return nil, fmt.Errorf("no topics found from any RSS feed")
	}

	return topics, nil
}

func truncateText(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func stripHTML(s string) string {
	var result []rune
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result = append(result, r)
		}
	}
	return string(result)
}

func extractKeywords(title string, feedTitle string) []string {
	seen := make(map[string]bool)
	var keywords []string
	add := func(w string) {
		if len(w) > 2 && !seen[w] {
			seen[w] = true
			keywords = append(keywords, w)
		}
	}

	words := splitWords(title)
	for _, w := range words {
		add(w)
	}

	if feedTitle != "" {
		feedWords := splitWords(feedTitle)
		for _, w := range feedWords {
			add(w)
		}
	}

	if len(keywords) > 8 {
		keywords = keywords[:8]
	}
	return keywords
}

func splitWords(s string) []string {
	var words []string
	current := []rune{}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			current = append(current, r)
		} else {
			if len(current) > 0 {
				words = append(words, string(current))
				current = nil
			}
		}
	}
	if len(current) > 0 {
		words = append(words, string(current))
	}
	return words
}
