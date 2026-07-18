package scheduler

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/agent"
)

// Scheduler runs the publish poller: it repeatedly asks the agent to publish
// the next due scheduled draft, which the agent posts using that draft owner's
// own LinkedIn tokens. Generation is user-driven (manual) in the multi-tenant
// model, so there is no auto-generate cron here.
type Scheduler struct {
	agent  *agent.Agent
	logger *zap.Logger

	// publishMu ensures only one publish pass runs at a time, so a slow
	// LinkedIn call can't overlap with the next tick and double-publish.
	publishMu sync.Mutex
}

// pollInterval controls how often we check for due scheduled posts.
const pollInterval = 2 * time.Second

func New(agent *agent.Agent, logger *zap.Logger) *Scheduler {
	return &Scheduler{agent: agent, logger: logger}
}

func (s *Scheduler) Start(ctx context.Context) error {
	go s.pollLoop(ctx)
	s.logger.Info("Publish scheduler started")
	return nil
}

func (s *Scheduler) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	s.publishPending(context.Background())

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.publishPending(context.Background())
		}
	}
}

func (s *Scheduler) publishPending(ctx context.Context) {
	// Skip this pass if a previous one is still running (e.g. a slow LinkedIn
	// call). Prevents overlapping passes from publishing the same draft twice.
	if !s.publishMu.TryLock() {
		return
	}
	defer s.publishMu.Unlock()

	if err := s.agent.PublishScheduled(ctx); err != nil {
		s.logger.Error("Scheduler: publish scheduled failed", zap.Error(err))
	}
}

func (s *Scheduler) Stop() {
	s.logger.Info("Scheduler stopped")
}
