package agent

import (
	"context"
	"errors"
	"fmt"
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
