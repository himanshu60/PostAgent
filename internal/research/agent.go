package research

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
)

type Agent struct {
	rss    *RSSFetcher
	news   *NewsAPIFetcher
	logger *zap.Logger
}

func NewAgent(rss *RSSFetcher, news *NewsAPIFetcher, logger *zap.Logger) *Agent {
	return &Agent{
		rss:    rss,
		news:   news,
		logger: logger,
	}
}

func (a *Agent) Fetch(ctx context.Context, count int) ([]FetchedTopic, error) {
	a.logger.Info("Starting research fetch", zap.Int("desired_count", count))

	var (
		mu     sync.Mutex
		topics []FetchedTopic
		seen   = make(map[string]bool)
		errs   []error
	)

	addUnique := func(t FetchedTopic) {
		mu.Lock()
		defer mu.Unlock()
		if !seen[t.URL] {
			seen[t.URL] = true
			topics = append(topics, t)
		}
	}

	var wg sync.WaitGroup

	if a.rss != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := a.rss.Fetch(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("rss: %w", err))
				mu.Unlock()
				return
			}
			for _, r := range results {
				addUnique(r)
			}
		}()
	}

	if a.news != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results, err := a.news.Fetch(ctx)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("newsapi: %w", err))
				mu.Unlock()
				return
			}
			for _, r := range results {
				addUnique(r)
			}
		}()
	}

	wg.Wait()

	a.logger.Info("Research fetch complete",
		zap.Int("total_unique", len(topics)),
		zap.Int("errors", len(errs)))

	if len(topics) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("all research sources failed: %v", errs)
	}

	if len(topics) > count {
		topics = topics[:count]
	}

	return topics, nil
}
