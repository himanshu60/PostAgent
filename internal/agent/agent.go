package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/analyzer"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/publisher"
	"github.com/clodura/linkedin-post-agent/internal/research"
	"github.com/clodura/linkedin-post-agent/internal/storage"
	"github.com/clodura/linkedin-post-agent/internal/ui"
)

type Agent struct {
	research     *research.Agent
	generator    *generator.LLMGenerator
	drafts       *storage.DraftRepo
	topics       *storage.TopicRepo
	published    *storage.PublishedRepo
	cycles       *storage.CycleRepo
	publisher    *publisher.Client
	voice        generator.BrandVoice
	exampleStore *analyzer.ExampleStore
	autoPublish  bool
	logger       *zap.Logger
}

func New(
	researchAgent *research.Agent,
	gen *generator.LLMGenerator,
	draftRepo *storage.DraftRepo,
	topicRepo *storage.TopicRepo,
	publishedRepo *storage.PublishedRepo,
	cycleRepo *storage.CycleRepo,
	pub *publisher.Client,
	voice generator.BrandVoice,
	exampleStore *analyzer.ExampleStore,
	autoPublish bool,
	logger *zap.Logger,
) *Agent {
	return &Agent{
		research:     researchAgent,
		generator:    gen,
		drafts:       draftRepo,
		topics:       topicRepo,
		published:    publishedRepo,
		cycles:       cycleRepo,
		publisher:    pub,
		voice:        voice,
		exampleStore: exampleStore,
		autoPublish:  autoPublish,
		logger:       logger,
	}
}

func (a *Agent) RunCycle(ctx context.Context, topicCount int) error {
	a.logger.Info("Starting agent cycle")

	cycle := &models.Cycle{
		Status: models.CycleStatusRunning,
	}
	if err := a.cycles.Insert(ctx, cycle); err != nil {
		return fmt.Errorf("create cycle: %w", err)
	}

	var topicModels []models.Topic
	var customTopics []models.Topic

	// User-added custom topics are an explicit queue — give them priority so
	// they're never silently starved out by ever-fresh trending topics (which
	// used to fill the whole topicCount quota first, every time).
	if ct, err := a.topics.GetUnused(ctx, "custom", topicCount); err == nil && len(ct) > 0 {
		a.logger.Info("Including custom topics", zap.Int("count", len(ct)))
		customTopics = ct
		topicModels = append(topicModels, ct...)
	}

	history, herr := a.topics.RecentTitles(ctx, topicHistoryWindow)
	if herr != nil {
		a.logger.Warn("Failed to load topic history for dedup, proceeding without it", zap.Error(herr))
	}

	if remaining := topicCount - len(topicModels); remaining > 0 {
		fetched, err := a.research.Fetch(ctx, dailyTopicPoolSize)
		if err != nil {
			if len(topicModels) == 0 {
				a.cycles.Skip(ctx, cycle.ID)
				return fmt.Errorf("research fetch: %w", err)
			}
			a.logger.Warn("Trending fetch failed, continuing with custom topics only", zap.Error(err))
		}
		added := 0
		for _, t := range a.relevantTrendingTopics(ctx, fetched) {
			if added >= remaining {
				break
			}
			// Skip topics that repeat something already covered.
			if research.IsDuplicateTitle(t.Title, history) {
				continue
			}
			model := t.ToModel()
			if err := a.topics.Insert(ctx, &model); err != nil {
				a.logger.Warn("Failed to store topic", zap.String("title", model.Title), zap.Error(err))
			}
			topicModels = append(topicModels, model)
			added++
		}
	}

	if len(topicModels) == 0 {
		a.logger.Warn("No topics found at all, skipping cycle")
		a.cycles.Skip(ctx, cycle.ID)
		return nil
	}

	styleGuide := a.loadStyleGuide(ctx)

	customByID := make(map[string]bool, len(customTopics))
	for _, t := range customTopics {
		customByID[t.ID] = true
	}

	var drafts []models.Draft
	var usedCustomIDs []string
	for _, topic := range topicModels {
		draft, err := a.generator.GenerateFromTopic(ctx, topic, a.voice, styleGuide)
		if err != nil {
			a.logger.Warn("Failed to generate draft",
				zap.String("topic", topic.Title),
				zap.Error(err))
			continue
		}
		draft.CycleID = cycle.ID
		drafts = append(drafts, *draft)
		if customByID[topic.ID] {
			usedCustomIDs = append(usedCustomIDs, topic.ID)
		}
	}

	if len(drafts) == 0 {
		a.logger.Warn("No drafts generated, skipping cycle")
		a.cycles.Skip(ctx, cycle.ID)
		return nil
	}

	a.logger.Info("Generated drafts", zap.Int("count", len(drafts)))

	// Only mark the custom topics we actually generated from as used, so capped
	// topics remain available for the next cycle.
	if len(usedCustomIDs) > 0 {
		if err := a.topics.MarkUsed(ctx, usedCustomIDs...); err != nil {
			a.logger.Warn("Failed to mark custom topics used", zap.Error(err))
		}
	}

	for i := range drafts {
		if err := a.drafts.Insert(ctx, &drafts[i]); err != nil {
			return fmt.Errorf("store draft: %w", err)
		}
		a.cycles.AddDraft(ctx, cycle.ID, drafts[i].ID)
	}

	if a.autoPublish {
		a.logger.Info("Auto-publishing first draft", zap.String("draft_id", drafts[0].ID))
		a.drafts.UpdateStatus(ctx, drafts[0].ID, models.DraftStatusApproved)
		result := &ui.SelectResult{
			Draft:  &drafts[0],
			Action: "publish",
		}
		return a.publishDraft(ctx, result)
	}

	result, err := ui.SelectDraft(drafts)
	if err != nil {
		a.cycles.Skip(ctx, cycle.ID)
		return fmt.Errorf("user selection: %w", err)
	}

	switch result.Action {
	case "skip":
		a.logger.Info("User skipped this cycle")
		a.cycles.Skip(ctx, cycle.ID)
		return nil

	case "restart":
		a.logger.Info("User requested restart")
		a.cycles.Skip(ctx, cycle.ID)
		return nil

	case "reject":
		a.logger.Info("User rejected all drafts")
		a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusRejected)
		a.cycles.Skip(ctx, cycle.ID)
		return nil

	case "edit":
		a.logger.Info("User edited and approved draft")
		a.drafts.UpdateBody(ctx, result.Draft.ID, result.Draft.Body)
		a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusApproved)
		return a.publishDraft(ctx, result)

	case "schedule":
		a.logger.Info("User scheduled draft", zap.Time("scheduled_for", *result.ScheduledFor))
		a.drafts.UpdateScheduledFor(ctx, result.Draft.ID, *result.ScheduledFor)
		a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusApproved)
		a.cycles.Complete(ctx, cycle.ID)
		return nil

	case "publish":
		a.logger.Info("User approved draft for publishing")
		a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusApproved)
		return a.publishDraft(ctx, result)

	default:
		a.logger.Warn("Unknown action, skipping", zap.String("action", result.Action))
		a.cycles.Skip(ctx, cycle.ID)
		return nil
	}
}

// dailyTopicPoolSize is how many trending candidates we pull before filtering
// out ones that duplicate previously covered topics. Larger than 1 so there's
// room to skip duplicates and still find something fresh.
const dailyTopicPoolSize = 25

// topicHistoryWindow caps how many past topic titles we compare new
// candidates against.
const topicHistoryWindow = 500

func (a *Agent) loadStyleGuide(ctx context.Context) string {
	if a.exampleStore == nil {
		return ""
	}
	examples, err := a.exampleStore.List(ctx, 10)
	if err != nil || len(examples) == 0 {
		return ""
	}
	pattern := analyzer.Analyze(examples)
	a.logger.Info("Style guide loaded from past posts", zap.Int("example_count", len(examples)))
	return pattern.PromptSuffix()
}

// relevantTrendingTopics filters fetched trending topics down to ones that
// fit the software-engineering audience (generator.TargetAudience). Uses one
// batched LLM call (result is also relevance-ranked); falls back to a keyword
// heuristic when the LLM is unavailable, so a quota outage degrades gracefully
// instead of letting off-topic consumer news through unfiltered.
func (a *Agent) relevantTrendingTopics(ctx context.Context, fetched []research.FetchedTopic) []research.FetchedTopic {
	if len(fetched) == 0 {
		return nil
	}
	titles := make([]string, len(fetched))
	for i, t := range fetched {
		titles[i] = t.Title
	}

	indices, err := a.generator.FilterRelevantTopics(ctx, titles)
	if err == nil {
		out := make([]research.FetchedTopic, 0, len(indices))
		for _, idx := range indices {
			out = append(out, fetched[idx])
		}
		return out
	}
	a.logger.Warn("LLM relevance filter unavailable, using keyword fallback", zap.Error(err))

	var out []research.FetchedTopic
	for _, t := range fetched {
		if research.IsDevTopic(t.Title, t.Summary) {
			out = append(out, t)
		}
	}
	a.logger.Info("Keyword relevance filter kept topics",
		zap.Int("candidates", len(fetched)), zap.Int("kept", len(out)))
	return out
}

// pickNonRepeatingTopic finds one topic that hasn't been covered before.
// The user's own custom-topic queue takes priority (explicit intent, no
// relevance filtering); trending topics are used next, filtered to the
// software-engineering audience. Both pools are checked against topic-title
// history (exact and near-duplicate) so the same subject is never picked twice.
func (a *Agent) pickNonRepeatingTopic(ctx context.Context) (topic *models.Topic, isCustom bool, err error) {
	history, herr := a.topics.RecentTitles(ctx, topicHistoryWindow)
	if herr != nil {
		a.logger.Warn("Failed to load topic history, proceeding without dedup context", zap.Error(herr))
	}

	custom, cerr := a.topics.GetUnused(ctx, "custom", 20)
	if cerr == nil {
		for i := range custom {
			if research.IsDuplicateTitle(custom[i].Title, history) {
				continue
			}
			if a.generator.IsSameTopic(ctx, custom[i].Title, history) {
				a.logger.Info("Skipping semantically duplicate custom topic", zap.String("title", custom[i].Title))
				continue
			}
			return &custom[i], true, nil
		}
	}

	fetched, ferr := a.research.Fetch(ctx, dailyTopicPoolSize)
	if ferr != nil {
		a.logger.Warn("Trending research fetch failed", zap.Error(ferr))
	}
	for _, t := range a.relevantTrendingTopics(ctx, fetched) {
		// Cheap word-overlap filter first (catches exact/near-exact repeats
		// with zero API cost), then a semantic LLM check as a backstop for
		// looser paraphrases the word filter would miss.
		if research.IsDuplicateTitle(t.Title, history) {
			continue
		}
		if exists, _ := a.topics.ExistsByURL(ctx, t.URL); exists {
			continue
		}
		if a.generator.IsSameTopic(ctx, t.Title, history) {
			a.logger.Info("Skipping semantically duplicate topic", zap.String("title", t.Title))
			continue
		}
		model := t.ToModel()
		return &model, false, nil
	}

	return nil, false, fmt.Errorf("no new, non-repeating topic found")
}

// GenerateAndSchedule generates exactly one post from a never-before-used
// topic and schedules it for publishAt (reusing the same approve+schedule
// mechanism as the UI, so Cancel Schedule / Publish Now / Change Time all
// work on it normally). Never auto-publishes immediately — the caller decides
// the target time, giving a review window before it goes live.
// DailyAutoTrigger tags cycles created by the once-a-day auto-generate job,
// distinguishing them from manually-triggered cycles (Run Cycle button, CLI).
const DailyAutoTrigger = "daily_auto"

func (a *Agent) GenerateAndSchedule(ctx context.Context, publishAt time.Time) error {
	a.logger.Info("Starting daily auto-generate cycle", zap.Time("publish_at", publishAt))

	cycle := &models.Cycle{Status: models.CycleStatusRunning, Trigger: DailyAutoTrigger}
	if err := a.cycles.Insert(ctx, cycle); err != nil {
		return fmt.Errorf("create cycle: %w", err)
	}

	topic, isCustom, err := a.pickNonRepeatingTopic(ctx)
	if err != nil {
		a.logger.Warn("Skipping today's auto post: no fresh topic available", zap.Error(err))
		a.cycles.Skip(ctx, cycle.ID)
		return err
	}

	// Persist the topic immediately so it's excluded from tomorrow's dedup
	// check even if draft generation below fails.
	if !isCustom {
		if err := a.topics.Insert(ctx, topic); err != nil {
			a.logger.Warn("Failed to store topic", zap.String("title", topic.Title), zap.Error(err))
		}
	}

	styleGuide := a.loadStyleGuide(ctx)

	draft, err := a.generator.GenerateFromTopic(ctx, *topic, a.voice, styleGuide)
	if err != nil {
		a.cycles.Skip(ctx, cycle.ID)
		return fmt.Errorf("generate draft: %w", err)
	}
	draft.CycleID = cycle.ID

	if err := a.drafts.Insert(ctx, draft); err != nil {
		a.cycles.Skip(ctx, cycle.ID)
		return fmt.Errorf("store draft: %w", err)
	}
	a.cycles.AddDraft(ctx, cycle.ID, draft.ID)

	if err := a.drafts.UpdateScheduledFor(ctx, draft.ID, publishAt); err != nil {
		a.logger.Error("Failed to set scheduled_for", zap.Error(err))
	}
	if err := a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusApproved); err != nil {
		a.logger.Error("Failed to approve draft", zap.Error(err))
	}
	if isCustom {
		a.topics.MarkUsed(ctx, topic.ID)
	}

	a.cycles.Complete(ctx, cycle.ID)
	a.logger.Info("Auto-generated and scheduled tomorrow's post",
		zap.String("draft_id", draft.ID),
		zap.String("topic", topic.Title),
		zap.Time("scheduled_for", publishAt))
	return nil
}

// EnsureDailyPost runs GenerateAndSchedule for the given publish slot, unless
// (a) the auto-generate job already ran today, or (b) the target day already
// has a scheduled post (e.g. the user manually scheduled or rescheduled one
// onto it) — enforcing at most one post per calendar day.
// Call this both from the cron trigger AND on a recurring safety check —
// it makes the job self-healing: if the process was asleep, restarted, or the
// machine was off at the exact scheduled moment, the next time this runs
// (e.g. on startup, or the next periodic check) it catches up instead of
// silently skipping.
func (a *Agent) EnsureDailyPost(ctx context.Context, publishAt time.Time, loc *time.Location) error {
	now := time.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	ran, err := a.cycles.HasRunSince(ctx, DailyAutoTrigger, startOfDay.UTC())
	if err != nil {
		a.logger.Warn("Failed to check daily-post history, running anyway", zap.Error(err))
	} else if ran {
		a.logger.Debug("Auto-post job already ran today, skipping")
		return nil
	}

	// One post per day: skip if the target day is already taken.
	pubLocal := publishAt.In(loc)
	dayStart := time.Date(pubLocal.Year(), pubLocal.Month(), pubLocal.Day(), 0, 0, 0, 0, loc)
	n, err := a.drafts.CountScheduledBetween(ctx, "", dayStart.UTC(), dayStart.Add(24*time.Hour).UTC())
	if err != nil {
		a.logger.Warn("Failed to check target-day conflicts, proceeding", zap.Error(err))
	} else if n > 0 {
		a.logger.Info("Target publish day already has a scheduled post, skipping auto-generate",
			zap.Time("publish_at", publishAt))
		return nil
	}

	return a.GenerateAndSchedule(ctx, publishAt)
}

func (a *Agent) publishDraft(ctx context.Context, result *ui.SelectResult) error {
	if a.publisher == nil || !a.publisher.IsConfigured() {
		a.logger.Warn("LinkedIn not configured, saving as draft in MongoDB instead of publishing")
		a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusPublished)
		a.cycles.Complete(ctx, result.Draft.CycleID)
		return nil
	}

	if err := a.publisher.RefreshToken(); err != nil {
		a.logger.Warn("Token refresh failed, proceeding with existing token", zap.Error(err))
	}

	postID, postURL, err := a.publisher.CreatePost(ctx, result.Draft)
	if err != nil {
		if errors.Is(err, publisher.ErrDuplicatePost) {
			// Content is already on LinkedIn (a prior attempt succeeded but we
			// failed to record it). Mark published so we stop retrying.
			a.logger.Warn("Draft already published on LinkedIn (duplicate), marking published",
				zap.String("draft_id", result.Draft.ID))
			a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusPublished)
			a.cycles.Complete(ctx, result.Draft.CycleID)
			return nil
		}
		a.logger.Error("Failed to publish", zap.Error(err))
		return fmt.Errorf("publish: %w", err)
	}

	published := &models.PublishedPost{
		DraftID:        result.Draft.ID,
		LinkedInPostID: postID,
		PostURL:        postURL,
	}

	if err := a.published.Insert(ctx, published); err != nil {
		a.logger.Error("Failed to store published record", zap.Error(err))
	}

	a.drafts.UpdateStatus(ctx, result.Draft.ID, models.DraftStatusPublished)
	a.cycles.Complete(ctx, result.Draft.CycleID)

	a.logger.Info("Published successfully",
		zap.String("post_url", postURL),
		zap.String("linkedin_id", postID))

	a.saveTokensToEnv()

	return nil
}

func (a *Agent) saveTokensToEnv() {
	at := a.publisher.AccessToken()
	rt := a.publisher.RefreshTokenStr()
	if at == "" {
		return
	}

	content, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	hadAT, hadRT := false, false
	for _, line := range lines {
		if strings.HasPrefix(line, "LINKEDIN_ACCESS_TOKEN=") {
			newLines = append(newLines, "LINKEDIN_ACCESS_TOKEN="+at)
			hadAT = true
		} else if strings.HasPrefix(line, "LINKEDIN_REFRESH_TOKEN=") {
			if rt != "" {
				newLines = append(newLines, "LINKEDIN_REFRESH_TOKEN="+rt)
			}
			hadRT = true
		} else {
			newLines = append(newLines, line)
		}
	}
	if !hadAT {
		newLines = append(newLines, "LINKEDIN_ACCESS_TOKEN="+at)
	}
	if !hadRT && rt != "" {
		newLines = append(newLines, "LINKEDIN_REFRESH_TOKEN="+rt)
	}

	if err := os.WriteFile(".env", []byte(strings.Join(newLines, "\n")), 0644); err != nil {
		a.logger.Warn("Failed to save tokens to .env", zap.Error(err))
	} else {
		a.logger.Info("Tokens saved to .env")
	}
}

func (a *Agent) PublishScheduled(ctx context.Context) error {
	draft, err := a.drafts.GetNextPending(ctx)
	if err != nil {
		return nil
	}

	a.logger.Info("Publishing scheduled draft", zap.String("draft_id", draft.ID))

	result := &ui.SelectResult{
		Draft:  draft,
		Action: "publish",
	}

	return a.publishDraft(ctx, result)
}
