package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/agent"
)

// Scheduler runs two loops:
//  1. a publish poller that posts each due scheduled draft with its owner's
//     LinkedIn tokens; and
//  2. a daily auto-generate job that, for every user, researches a fresh topic,
//     generates a post, and schedules it for the next publish day.
//
// The auto-generate job is idempotent and self-healing: it runs from a cron
// tick, an immediate startup catch-up, and a periodic safety net, so a missed
// exact moment (service asleep/restarted) gets caught up on the next check.
type Scheduler struct {
	agent       *agent.Agent
	cron        *cron.Cron
	schedule    string
	publishDays []time.Weekday
	logger      *zap.Logger

	publishMu sync.Mutex
	dailyMu   sync.Mutex
}

const (
	pollInterval       = 2 * time.Second
	dailyCheckInterval = 20 * time.Minute
)

func istLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.UTC
	}
	return loc
}

func New(agent *agent.Agent, schedule string, publishDays []time.Weekday, logger *zap.Logger) *Scheduler {
	return &Scheduler{
		agent:       agent,
		cron:        cron.New(cron.WithSeconds()),
		schedule:    schedule,
		publishDays: publishDays,
		logger:      logger,
	}
}

// nextPublishSlot returns 10:00 IST on the next configured publish day (starting
// tomorrow, so there's a review window), as UTC.
func (s *Scheduler) nextPublishSlot() time.Time {
	loc := istLocation()
	now := time.Now().In(loc)
	isPublishDay := func(d time.Weekday) bool {
		if len(s.publishDays) == 0 {
			return true
		}
		for _, pd := range s.publishDays {
			if pd == d {
				return true
			}
		}
		return false
	}
	for offset := 1; offset <= 8; offset++ {
		day := time.Date(now.Year(), now.Month(), now.Day()+offset, 10, 0, 0, 0, loc)
		if isPublishDay(day.Weekday()) {
			return day.UTC()
		}
	}
	return time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, loc).UTC()
}

func (s *Scheduler) Start(ctx context.Context) error {
	if s.schedule != "" {
		if _, err := s.cron.AddFunc(s.schedule, func() { s.ensureDaily("cron") }); err != nil {
			return err
		}
		s.cron.Start()
	}
	s.logger.Info("Scheduler started", zap.String("schedule", s.schedule))

	go s.ensureDaily("startup") // catch up if the service was down at the cron moment
	go s.pollLoop(ctx)          // publish due scheduled posts
	go s.dailyCheckLoop(ctx)    // periodic safety net
	return nil
}

func (s *Scheduler) ensureDaily(trigger string) {
	if !s.dailyMu.TryLock() {
		return
	}
	defer s.dailyMu.Unlock()

	c, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	s.logger.Info("Auto-generate check", zap.String("trigger", trigger))
	s.agent.EnsureDailyPosts(c, s.nextPublishSlot(), istLocation())
}

func (s *Scheduler) dailyCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(dailyCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ensureDaily("periodic")
		}
	}
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
	if !s.publishMu.TryLock() {
		return
	}
	defer s.publishMu.Unlock()
	if err := s.agent.PublishScheduled(ctx); err != nil {
		s.logger.Error("Scheduler: publish scheduled failed", zap.Error(err))
	}
}

func (s *Scheduler) Stop() {
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
	}
	s.logger.Info("Scheduler stopped")
}
