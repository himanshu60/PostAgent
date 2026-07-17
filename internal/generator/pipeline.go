package generator

import (
	"context"
	"fmt"
	"math/rand"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/clodura/linkedin-post-agent/internal/llm"
	"github.com/clodura/linkedin-post-agent/internal/models"
)

type LLMGenerator struct {
	llm    llm.Client
	logger *zap.Logger
}

func NewLLMGenerator(llmClient llm.Client, logger *zap.Logger) *LLMGenerator {
	return &LLMGenerator{
		llm:    llmClient,
		logger: logger,
	}
}

// ideaCategoryPool is rotated through to actively steer idea generation away
// from the model's default go-to topics (which converge on the same handful
// of buzzwords when called repeatedly with a similar/vague query).
var ideaCategoryPool = []string{
	"real-world engineering tradeoffs",
	"career growth and promotion strategy",
	"developer tools and workflow improvements",
	"system architecture decisions",
	"common mistakes and lessons learned",
	"industry trends and predictions",
	"team culture and leadership",
	"testing and code quality",
	"cost and performance optimization",
	"security and reliability",
	"cloud infrastructure and DevOps",
	"data engineering and databases",
	"emerging languages and frameworks",
	"technical debt and refactoring",
	"interviewing and hiring practices",
	"remote work and productivity",
}

// pickRandomCategories returns n distinct categories in random order.
func pickRandomCategories(n int) []string {
	pool := append([]string{}, ideaCategoryPool...)
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	if n > len(pool) {
		n = len(pool)
	}
	return pool[:n]
}

func (g *LLMGenerator) GenerateTopicIdeas(ctx context.Context, query string, exampleBodies []string, avoidTitles []string) ([]TopicIdea, error) {
	focus := pickRandomCategories(4)
	prompt := topicIdeaPrompt(query, exampleBodies, avoidTitles, focus)
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a LinkedIn content strategist. Generate topic ideas that match the user's writing style."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate topic ideas: %w", err)
	}
	return parseTopicIdeas(resp.Content), nil
}

func parseTopicIdeas(text string) []TopicIdea {
	var ideas []TopicIdea
	var current TopicIdea
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Title != "" {
				ideas = append(ideas, current)
				current = TopicIdea{}
			}
			continue
		}
		if strings.HasPrefix(line, "TOPIC:") {
			current.Title = strings.TrimSpace(strings.TrimPrefix(line, "TOPIC:"))
		} else if strings.HasPrefix(line, "HOOK:") {
			current.Hook = strings.TrimSpace(strings.TrimPrefix(line, "HOOK:"))
		} else if strings.HasPrefix(line, "WHY:") {
			current.Explanation = strings.TrimSpace(strings.TrimPrefix(line, "WHY:"))
		}
	}
	if current.Title != "" {
		ideas = append(ideas, current)
	}
	return ideas
}

func (g *LLMGenerator) RefineTopic(ctx context.Context, input TopicRefinerInput) ([]RefinedAngle, error) {
	prompt := topicRefinerPrompt(input)
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a LinkedIn content strategist. Respond only with the requested format."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("refine topic: %w", err)
	}
	return parseAngles(resp.Content)
}

func (g *LLMGenerator) WriteDraft(ctx context.Context, input DraftWriterInput) (string, error) {
	prompt := draftWriterPrompt(input)
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a LinkedIn content creator. Write authentic, engaging posts."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("write draft: %w", err)
	}
	return resp.Content, nil
}

func (g *LLMGenerator) PolishDraft(ctx context.Context, input PolisherInput) (*PolishedOutput, error) {
	prompt := polisherPrompt(input)
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a LinkedIn post editor. Polish and optimize for engagement."},
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("polish draft: %w", err)
	}
	return parsePolished(resp.Content)
}

func (g *LLMGenerator) GenerateFromTopic(ctx context.Context, topic models.Topic, voice BrandVoice, styleGuide string) (*models.Draft, error) {
	g.logger.Info("Generating post from topic", zap.String("title", topic.Title))

	// Single LLM call: structure, polish, and hashtags together. The old
	// refine→write→polish pipeline made 3 calls per post, which was far too
	// slow on a local model.
	resp, err := g.llm.Chat(ctx, llm.ChatRequest{
		Messages: []llm.Message{
			{Role: "system", Content: "You are a LinkedIn content creator. Write authentic, engaging, well-structured posts."},
			{Role: "user", Content: singleShotPrompt(topic.Title, topic.Summary, voice.Tone, styleGuide, voice.MaxEmoji, pickRandomStructure())},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("generate post: %w", err)
	}

	polished, err := parsePolished(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse post: %w", err)
	}

	draft := &models.Draft{
		ID:       uuid.New().String(),
		TopicID:  topic.ID,
		Title:    topic.Title,
		Body:     boldHeadings(convertMarkdownBold(normalizeBody(polished.Body))),
		Hashtags: ensureHashtags(polished.Hashtags, topic.Keywords),
		Status:   models.DraftStatusDraft,
	}

	// Attach a topic-relevant image. We store both a prompt (used by OpenAI at
	// publish time when configured) and a Pollinations URL (free fallback that
	// also works for the preview). The image is uploaded to LinkedIn at publish.
	scene := g.imageSceneForTopic(ctx, topic)
	draft.Media = &models.Media{
		Type:      models.MediaTypeImage,
		Prompt:    openAIImagePrompt(topic.Title),
		SourceURL: pollinationsImageURL(pollinationsPrompt(scene, topic.Title), stableSeed(topic.Title)),
		AltText:   topic.Title,
	}

	g.logger.Info("Generated draft",
		zap.String("draft_id", draft.ID),
		zap.Int("body_len", len(draft.Body)),
		zap.Int("hashtags", len(draft.Hashtags)))

	return draft, nil
}

func parseAngles(text string) ([]RefinedAngle, error) {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var angles []RefinedAngle
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, fmt.Sprintf("Angle %d:", i+1))
		line = strings.TrimPrefix(line, fmt.Sprintf("%d.", i+1))
		line = strings.TrimSpace(line)

		parts := strings.SplitN(line, "|", 2)
		if len(parts) >= 2 {
			angles = append(angles, RefinedAngle{
				Index: i,
				Title: strings.TrimSpace(parts[0]),
				Hook:  strings.TrimSpace(parts[1]),
			})
		}
	}
	if len(angles) == 0 {
		angles = append(angles, RefinedAngle{
			Index: 0,
			Title: "Default angle",
			Hook:  text,
		})
	}
	return angles, nil
}

func stripPostPrefixes(text string) string {
	result := strings.TrimSpace(text)

	// Strip a leading meta line such as "Here is the polished LinkedIn post ...:"
	// regardless of the exact wording, as long as it's a short intro ending in ":".
	for {
		firstLine := result
		if nl := strings.IndexByte(result, '\n'); nl >= 0 {
			firstLine = result[:nl]
		}
		trimmed := strings.TrimSpace(firstLine)
		lower := strings.ToLower(trimmed)
		isMeta := strings.HasSuffix(trimmed, ":") && len(trimmed) < 90 &&
			(strings.HasPrefix(lower, "here is") || strings.HasPrefix(lower, "here's") ||
				strings.HasPrefix(lower, "here are") || strings.HasPrefix(lower, "sure") ||
				strings.HasPrefix(lower, "below is") || strings.HasPrefix(lower, "of course"))
		if !isMeta {
			break
		}
		nl := strings.IndexByte(result, '\n')
		if nl < 0 {
			return ""
		}
		result = strings.TrimSpace(result[nl+1:])
	}
	text = result

	prefixes := []string{
		"Here is the polished LinkedIn post:",
		"Here's the polished LinkedIn post:",
		"Here is the polished post:",
		"Here's the polished post:",
		"Here is your LinkedIn post:",
		"Here's your LinkedIn post:",
		"Here is the post:",
		"Here's the post:",
		"Here is your post:",
		"Here's your post:",
		"Polished post:",
		"**Polished Post:**",
		"**Polished post:**",
		"Here is the polished version:",
		"The polished LinkedIn post:",
		"LinkedIn Post:",
	}
	result = strings.TrimSpace(text)
	for {
		stripped := false
		for _, p := range prefixes {
			if strings.HasPrefix(strings.TrimSpace(result), p) {
				result = strings.TrimSpace(result[len(p):])
				stripped = true
				break
			}
		}
		if !stripped {
			break
		}
	}
	return result
}

func parsePolished(text string) (*PolishedOutput, error) {
	cleaned := stripPostPrefixes(text)
	parts := strings.SplitN(cleaned, "---", 2)
	body := strings.TrimSpace(parts[0])

	var hashtags []string
	if len(parts) > 1 {
		rest := strings.TrimSpace(parts[1])
		for _, word := range strings.Fields(rest) {
			if strings.HasPrefix(word, "#") {
				hashtags = append(hashtags, word)
			}
		}
	}

	// Models sometimes put hashtags inline at the end of the body instead of
	// (or as well as) after "---". Strip trailing hashtag-only lines from the
	// body and merge them, so hashtags never appear twice in the final post.
	lines := strings.Split(body, "\n")
	for len(lines) > 0 {
		last := strings.TrimSpace(lines[len(lines)-1])
		if last == "" {
			lines = lines[:len(lines)-1]
			continue
		}
		if !isHashtagLine(last) {
			break
		}
		for _, word := range strings.Fields(last) {
			if strings.HasPrefix(word, "#") {
				hashtags = append(hashtags, word)
			}
		}
		lines = lines[:len(lines)-1]
	}
	body = strings.TrimSpace(strings.Join(lines, "\n"))

	return &PolishedOutput{
		Body:     body,
		Hashtags: hashtags,
	}, nil
}

// isHashtagLine reports whether a line is (almost) entirely hashtags.
func isHashtagLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	tags := 0
	for _, f := range fields {
		if strings.HasPrefix(f, "#") {
			tags++
		}
	}
	return tags*100/len(fields) >= 80
}
