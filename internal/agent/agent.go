package agent

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/analyzer"
	"github.com/clodura/linkedin-post-agent/internal/generator"
	"github.com/clodura/linkedin-post-agent/internal/models"
	"github.com/clodura/linkedin-post-agent/internal/publisher"
	"github.com/clodura/linkedin-post-agent/internal/research"
	"github.com/clodura/linkedin-post-agent/internal/storage"
)

// PublisherFactory builds a LinkedIn publisher configured with a specific
// user's OAuth tokens, so every post is authored by that user's own account.
type PublisherFactory func(u *models.User) *publisher.Client

type Agent struct {
	research     *research.Agent
	generator    *generator.LLMGenerator
	drafts       *storage.DraftRepo
	topics       *storage.TopicRepo
	published    *storage.PublishedRepo
	cycles       *storage.CycleRepo
	users        *storage.UserRepo
	voice        generator.BrandVoice
	exampleStore *analyzer.ExampleStore
	newPublisher PublisherFactory
	logger       *zap.Logger
}

func New(
	researchAgent *research.Agent,
	gen *generator.LLMGenerator,
	draftRepo *storage.DraftRepo,
	topicRepo *storage.TopicRepo,
	publishedRepo *storage.PublishedRepo,
	cycleRepo *storage.CycleRepo,
	userRepo *storage.UserRepo,
	voice generator.BrandVoice,
	exampleStore *analyzer.ExampleStore,
	newPublisher PublisherFactory,
	logger *zap.Logger,
) *Agent {
	return &Agent{
		research:     researchAgent,
		generator:    gen,
		drafts:       draftRepo,
		topics:       topicRepo,
		published:    publishedRepo,
		cycles:       cycleRepo,
		users:        userRepo,
		voice:        voice,
		exampleStore: exampleStore,
		newPublisher: newPublisher,
		logger:       logger,
	}
}

// dailyTopicPoolSize is how many trending candidates we pull before filtering
// out ones that duplicate previously covered topics.
const dailyTopicPoolSize = 25

// topicHistoryWindow caps how many past topic titles we compare new
// candidates against.
const topicHistoryWindow = 500

// Generate runs one research→generate cycle for a single user and stores the
// resulting drafts (scoped to that user). It never publishes — the user reviews
// and schedules from the UI, and the scheduler publishes with the user's own
// tokens. Returns the drafts it created.
func (a *Agent) Generate(ctx context.Context, userID string, topicCount int) ([]models.Draft, error) {
	if userID == "" {
		return nil, fmt.Errorf("generate requires a user")
	}
	a.logger.Info("Starting generate cycle", zap.String("user_id", userID))

	cycle := &models.Cycle{UserID: userID, Status: models.CycleStatusRunning, Trigger: "manual"}
	if err := a.cycles.Insert(ctx, cycle); err != nil {
		return nil, fmt.Errorf("create cycle: %w", err)
	}

	var topicModels []models.Topic
	var customTopics []models.Topic

	// User-added custom topics take priority over ever-fresh trending topics.
	if ct, err := a.topics.GetUnused(ctx, userID, "custom", topicCount); err == nil && len(ct) > 0 {
		customTopics = ct
		topicModels = append(topicModels, ct...)
	}

	history, _ := a.topics.RecentTitles(ctx, userID, topicHistoryWindow)

	if remaining := topicCount - len(topicModels); remaining > 0 {
		fetched, err := a.research.Fetch(ctx, dailyTopicPoolSize)
		if err != nil && len(topicModels) == 0 {
			a.cycles.Skip(ctx, cycle.ID)
			return nil, fmt.Errorf("research fetch: %w", err)
		}
		added := 0
		for _, t := range a.relevantTrendingTopics(ctx, fetched) {
			if added >= remaining {
				break
			}
			if research.IsDuplicateTitle(t.Title, history) {
				continue
			}
			model := t.ToModel()
			model.UserID = userID
			if err := a.topics.Insert(ctx, &model); err != nil {
				a.logger.Warn("Failed to store topic", zap.String("title", model.Title), zap.Error(err))
			}
			topicModels = append(topicModels, model)
			added++
		}
	}

	if len(topicModels) == 0 {
		a.cycles.Skip(ctx, cycle.ID)
		return nil, nil
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
			a.logger.Warn("Failed to generate draft", zap.String("topic", topic.Title), zap.Error(err))
			continue
		}
		draft.UserID = userID
		draft.CycleID = cycle.ID
		drafts = append(drafts, *draft)
		if customByID[topic.ID] {
			usedCustomIDs = append(usedCustomIDs, topic.ID)
		}
	}

	if len(drafts) == 0 {
		a.cycles.Skip(ctx, cycle.ID)
		return nil, nil
	}

	if len(usedCustomIDs) > 0 {
		if err := a.topics.MarkUsed(ctx, usedCustomIDs...); err != nil {
			a.logger.Warn("Failed to mark custom topics used", zap.Error(err))
		}
	}

	for i := range drafts {
		if err := a.drafts.Insert(ctx, &drafts[i]); err != nil {
			return drafts, fmt.Errorf("store draft: %w", err)
		}
		a.cycles.AddDraft(ctx, cycle.ID, drafts[i].ID)
	}

	a.cycles.Complete(ctx, cycle.ID)
	a.logger.Info("Generated drafts", zap.String("user_id", userID), zap.Int("count", len(drafts)))
	return drafts, nil
}

// DailyAutoTrigger tags cycles created by the once-a-day auto-generate job.
const DailyAutoTrigger = "daily_auto"

// EnsureDailyPosts runs the idempotent per-user auto-generate for every user:
// for each, if they don't already have a post scheduled for the target day (and
// today's auto job hasn't run for them), research a fresh topic, generate a
// post, and schedule it for publishAt. Safe to call from cron/startup/periodic.
func (a *Agent) EnsureDailyPosts(ctx context.Context, publishAt time.Time, loc *time.Location) {
	users, err := a.users.List(ctx)
	if err != nil {
		a.logger.Warn("Auto-generate: failed to list users", zap.Error(err))
		return
	}
	for i := range users {
		a.ensureDailyPostForUser(ctx, &users[i], publishAt, loc)
	}
}

func (a *Agent) ensureDailyPostForUser(ctx context.Context, user *models.User, publishAt time.Time, loc *time.Location) {
	now := time.Now().In(loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	// Already auto-generated for this user today? Skip (idempotent / self-healing).
	if ran, err := a.cycles.HasRunSince(ctx, user.ID, DailyAutoTrigger, startOfDay.UTC()); err == nil && ran {
		return
	}
	// Target day already has a scheduled post for this user? Skip (one/day).
	pubLocal := publishAt.In(loc)
	dayStart := time.Date(pubLocal.Year(), pubLocal.Month(), pubLocal.Day(), 0, 0, 0, 0, loc)
	if n, err := a.drafts.CountScheduledBetween(ctx, user.ID, "", dayStart.UTC(), dayStart.Add(24*time.Hour).UTC()); err == nil && n > 0 {
		return
	}

	cycle := &models.Cycle{UserID: user.ID, Status: models.CycleStatusRunning, Trigger: DailyAutoTrigger}
	if err := a.cycles.Insert(ctx, cycle); err != nil {
		a.logger.Warn("Auto-generate: create cycle failed", zap.String("user_id", user.ID), zap.Error(err))
		return
	}

	// Prefer the user's own interests; fall back to trending if none set.
	var topic *models.Topic
	var err error
	if len(user.Interests) > 0 {
		topic, err = a.topicFromInterest(ctx, user)
	} else {
		topic, err = a.pickFreshTopicForUser(ctx, user.ID)
	}
	if err != nil || topic == nil {
		a.logger.Info("Auto-generate: no fresh topic, skipping", zap.String("user_id", user.ID))
		a.cycles.Skip(ctx, cycle.ID)
		return
	}
	topic.UserID = user.ID
	if err := a.topics.Insert(ctx, topic); err != nil {
		a.logger.Warn("Auto-generate: store topic failed", zap.Error(err))
	}

	// Style guide + the user's designation so the post reads from their POV.
	styleGuide := a.loadStyleGuide(ctx)
	if user.Role != "" {
		styleGuide += "\n\nWrite from the perspective of a " + user.Role + ", sharing practical, first-hand experience from that role."
	}
	draft, err := a.generator.GenerateFromTopic(ctx, *topic, a.voice, styleGuide)
	if err != nil {
		a.cycles.Skip(ctx, cycle.ID)
		return
	}
	draft.UserID = user.ID
	draft.CycleID = cycle.ID
	if err := a.drafts.Insert(ctx, draft); err != nil {
		a.cycles.Skip(ctx, cycle.ID)
		return
	}
	a.cycles.AddDraft(ctx, cycle.ID, draft.ID)
	a.drafts.UpdateScheduledFor(ctx, draft.ID, publishAt)
	a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusApproved)
	a.cycles.Complete(ctx, cycle.ID)
	a.logger.Info("Auto-generated and scheduled a post",
		zap.String("user_id", user.ID), zap.String("draft_id", draft.ID), zap.Time("publish_at", publishAt))
}

// topicFromInterest turns one of the user's chosen interests into a fresh,
// specific post topic (so their auto-posts stay in their field and on-brand).
func (a *Agent) topicFromInterest(ctx context.Context, user *models.User) (*models.Topic, error) {
	interest := user.Interests[rand.Intn(len(user.Interests))]
	history, _ := a.topics.RecentTitles(ctx, user.ID, topicHistoryWindow)
	ideas, err := a.generator.GenerateTopicIdeas(ctx, interest, nil, history)
	if err == nil {
		for _, idea := range ideas {
			if research.IsDuplicateTitle(idea.Title, history) {
				continue
			}
			return &models.Topic{Title: idea.Title, Summary: idea.Hook, Source: "interest"}, nil
		}
		if len(ideas) > 0 {
			return &models.Topic{Title: ideas[0].Title, Summary: ideas[0].Hook, Source: "interest"}, nil
		}
	}
	// Fallback: use the interest itself as the topic.
	return &models.Topic{Title: interest, Summary: "A practical LinkedIn post about " + interest, Source: "interest"}, nil
}

// pickFreshTopicForUser finds one trending topic this user hasn't covered.
// The relevant pool is shuffled so different users (and different days) get
// different topics from the same trending feed — avoiding two users' auto-posts
// landing on the identical subject.
func (a *Agent) pickFreshTopicForUser(ctx context.Context, userID string) (*models.Topic, error) {
	history, _ := a.topics.RecentTitles(ctx, userID, topicHistoryWindow)
	fetched, _ := a.research.Fetch(ctx, dailyTopicPoolSize)
	relevant := a.relevantTrendingTopics(ctx, fetched)
	rand.Shuffle(len(relevant), func(i, j int) { relevant[i], relevant[j] = relevant[j], relevant[i] })
	for _, t := range relevant {
		if research.IsDuplicateTitle(t.Title, history) {
			continue
		}
		if exists, _ := a.topics.ExistsByURL(ctx, userID, t.URL); exists {
			continue
		}
		model := t.ToModel()
		return &model, nil
	}
	return nil, fmt.Errorf("no fresh topic")
}

func (a *Agent) loadStyleGuide(ctx context.Context) string {
	if a.exampleStore == nil {
		return ""
	}
	examples, err := a.exampleStore.List(ctx, 10)
	if err != nil || len(examples) == 0 {
		return ""
	}
	pattern := analyzer.Analyze(examples)
	return pattern.PromptSuffix()
}

// relevantTrendingTopics filters fetched trending topics down to ones that fit
// a software-engineering audience, with a keyword fallback if the LLM is down.
func (a *Agent) relevantTrendingTopics(ctx context.Context, fetched []research.FetchedTopic) []research.FetchedTopic {
	if len(fetched) == 0 {
		return nil
	}
	titles := make([]string, len(fetched))
	for i, t := range fetched {
		titles[i] = t.Title
	}
	if indices, err := a.generator.FilterRelevantTopics(ctx, titles); err == nil {
		out := make([]research.FetchedTopic, 0, len(indices))
		for _, idx := range indices {
			out = append(out, fetched[idx])
		}
		return out
	}
	var out []research.FetchedTopic
	for _, t := range fetched {
		if research.IsDevTopic(t.Title, t.Summary) {
			out = append(out, t)
		}
	}
	return out
}

// PublishScheduled publishes the next due scheduled draft (across all users),
// authoring it with that draft owner's own LinkedIn tokens. Refreshed tokens
// are persisted back to the user so their session stays valid.
func (a *Agent) PublishScheduled(ctx context.Context) error {
	draft, err := a.drafts.GetNextPending(ctx)
	if err != nil {
		return nil // nothing due
	}
	if draft.UserID == "" {
		a.logger.Warn("Scheduled draft has no owner, marking published to avoid a stuck queue",
			zap.String("draft_id", draft.ID))
		a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusPublished)
		return nil
	}

	user, uerr := a.users.GetByID(ctx, draft.UserID)
	if uerr != nil {
		a.logger.Error("Cannot publish: owner not found", zap.String("user_id", draft.UserID), zap.Error(uerr))
		return nil
	}

	return a.publishDraftAs(ctx, draft, user)
}

// publishDraftAs posts a draft to LinkedIn using the given user's publisher.
func (a *Agent) publishDraftAs(ctx context.Context, draft *models.Draft, user *models.User) error {
	pub := a.newPublisher(user)
	if pub == nil || !pub.IsConfigured() {
		a.logger.Warn("User's LinkedIn not configured, marking draft published without posting",
			zap.String("user_id", user.ID))
		a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusPublished)
		a.cycles.Complete(ctx, draft.CycleID)
		return nil
	}

	if err := pub.RefreshToken(); err != nil {
		a.logger.Warn("Token refresh failed, proceeding with existing token", zap.Error(err))
	}

	postID, postURL, err := pub.CreatePost(ctx, draft)
	if err != nil {
		if errors.Is(err, publisher.ErrDuplicatePost) {
			a.logger.Warn("Draft already on LinkedIn (duplicate), marking published",
				zap.String("draft_id", draft.ID))
			a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusPublished)
			a.cycles.Complete(ctx, draft.CycleID)
			return nil
		}
		a.logger.Error("Failed to publish", zap.String("user_id", user.ID), zap.Error(err))
		return fmt.Errorf("publish: %w", err)
	}

	published := &models.PublishedPost{
		UserID:         user.ID,
		DraftID:        draft.ID,
		LinkedInPostID: postID,
		PostURL:        postURL,
		PostedAt:       time.Now(),
	}
	if err := a.published.Insert(ctx, published); err != nil {
		a.logger.Error("Failed to store published record", zap.Error(err))
	}

	a.drafts.UpdateStatus(ctx, draft.ID, models.DraftStatusPublished)
	a.cycles.Complete(ctx, draft.CycleID)

	// Persist any refreshed tokens back to the user record.
	if at := pub.AccessToken(); at != "" && at != user.AccessToken {
		if err := a.users.UpdateTokens(ctx, user.ID, at, pub.RefreshTokenStr(), time.Now().Add(50*24*time.Hour)); err != nil {
			a.logger.Warn("Failed to persist refreshed tokens", zap.Error(err))
		}
	}

	a.logger.Info("Published successfully",
		zap.String("user_id", user.ID),
		zap.String("post_url", postURL))
	return nil
}
