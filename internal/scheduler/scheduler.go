package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/agent"
)

type Scheduler struct {
	agent       *agent.Agent
	cron        *cron.Cron
	schedule    string
	topicCount  int
	publishDays []time.Weekday
	logger      *zap.Logger

	mu          sync.Mutex
	timers      map[string]*time.Timer

	// publishMu ensures only one publish pass runs at a time, so a slow
	// LinkedIn call can't overlap with the next tick and double-publish.
	publishMu   sync.Mutex

	// dailyMu ensures only one "check + maybe generate today's post" attempt
	// runs at a time, so the cron firing, the startup catch-up, and the
	// periodic safety-net check can never race into a duplicate post.
	dailyMu     sync.Mutex
}

// pollInterval controls how often we check for due scheduled posts.
// Kept short so posts publish within a couple seconds of their scheduled time.
const pollInterval = 2 * time.Second

// dailyCheckInterval is the safety-net cadence for re-verifying today's
// auto-post happened. Catches cases where the process was asleep/down at the
// exact cron moment but is up again later — the job self-heals instead of
// silently skipping the day.
const dailyCheckInterval = 20 * time.Minute

// istLocation is where "today" and "10 AM" are evaluated for the daily job.
func istLocation() *time.Location {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		return time.UTC
	}
	return loc
}

// nextPublishSlot returns 10:00 IST on the next configured publish day,
// starting from tomorrow (generation always happens at least a day ahead so
// there's a review window to edit/cancel). Returned as UTC (the format
// scheduled_for is stored in). With no publish days configured, every day
// qualifies (daily cadence).
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
	// Unreachable (8 days always covers a week), but fall back to tomorrow.
	return time.Date(now.Year(), now.Month(), now.Day()+1, 10, 0, 0, 0, loc).UTC()
}

func New(
	agent *agent.Agent,
	schedule string,
	topicCount int,
	publishDays []time.Weekday,
	logger *zap.Logger,
) *Scheduler {
	return &Scheduler{
		agent:       agent,
		cron:        cron.New(cron.WithSeconds()),
		schedule:    schedule,
		topicCount:  topicCount,
		publishDays: publishDays,
		logger:      logger,
		timers:      make(map[string]*time.Timer),
	}
}

func (s *Scheduler) Start(ctx context.Context) error {
	// Daily job: generate exactly one post from a never-before-used trending
	// topic and auto-schedule it for 10 AM the next day (not published
	// immediately) — giving a review window to cancel/edit before it goes live.
	//
	// Runs through ensureDailyPost (not a direct call) from three places —
	// the cron trigger, an immediate startup catch-up, and a periodic
	// safety-net check — so a missed exact-9am moment (machine asleep,
	// process restarted, terminal closed) still gets caught up later instead
	// of silently skipping the day.
	_, err := s.cron.AddFunc(s.schedule, func() {
		s.ensureDailyPost("cron")
	})
	if err != nil {
		return err
	}

	s.cron.Start()
	s.logger.Info("Scheduler started", zap.String("schedule", s.schedule))

	// Catch up immediately if the process starts after today's slot already
	// passed (e.g. laptop was asleep at 9am, or the server was down/restarted).
	go s.ensureDailyPost("startup")

	// Poll for due scheduled posts every couple seconds.
	go s.pollLoop(ctx)

	// Safety net: keep re-checking throughout the day in case the cron tick
	// or startup catch-up both missed (e.g. a crash mid-generation).
	go s.dailyCheckLoop(ctx)

	return nil
}

// ensureDailyPost runs the idempotent daily-post check (safe to call from
// multiple triggers — cron, startup, periodic — since it's guarded both by
// dailyMu here and by the DB-backed "did today already run?" check inside
// agent.EnsureDailyPost).
func (s *Scheduler) ensureDailyPost(trigger string) {
	if !s.dailyMu.TryLock() {
		s.logger.Debug("Daily post check already in progress, skipping", zap.String("trigger", trigger))
		return
	}
	defer s.dailyMu.Unlock()

	s.logger.Info("Scheduler: checking daily auto-generate", zap.String("trigger", trigger))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := s.agent.EnsureDailyPost(ctx, s.nextPublishSlot(), istLocation()); err != nil {
		s.logger.Error("Scheduler: auto-generate failed", zap.String("trigger", trigger), zap.Error(err))
	}

	s.publishPending(ctx)
}

func (s *Scheduler) dailyCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(dailyCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.ensureDailyPost("periodic")
		}
	}
}

func (s *Scheduler) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	// Also check immediately on startup
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
	// call). This prevents overlapping passes from publishing the same draft twice.
	if !s.publishMu.TryLock() {
		return
	}
	defer s.publishMu.Unlock()

	if err := s.agent.PublishScheduled(ctx); err != nil {
		s.logger.Error("Scheduler: publish scheduled failed", zap.Error(err))
	} else {
		s.logger.Debug("Scheduler: publish check complete")
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	for id, t := range s.timers {
		t.Stop()
		delete(s.timers, id)
	}
	s.mu.Unlock()

	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("Scheduler stopped")
}

func (s *Scheduler) RunOnce(ctx context.Context) error {
	s.logger.Info("Running single cycle")

	if err := s.agent.RunCycle(ctx, s.topicCount); err != nil {
		return err
	}

	if err := s.agent.PublishScheduled(ctx); err != nil {
		s.logger.Error("Publish scheduled failed", zap.Error(err))
	}

	return nil
}
