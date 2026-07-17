package generator

import (
	"context"

	"github.com/clodura/linkedin-post-agent/internal/models"
)

type BrandVoice struct {
	Tone          string
	MaxEmoji      int
	MinLineBreaks int
}

func BrandVoiceFromConfig(cfg interface {
	GetTone() string
	GetMaxEmoji() int
	GetMinLineBreaks() int
}) BrandVoice {
	return BrandVoice{
		Tone:          cfg.GetTone(),
		MaxEmoji:      cfg.GetMaxEmoji(),
		MinLineBreaks: cfg.GetMinLineBreaks(),
	}
}

type TopicRefinerInput struct {
	Title    string
	Summary  string
	Keywords []string
}

type RefinedAngle struct {
	Index int
	Title string
	Hook  string
}

type DraftWriterInput struct {
	Title      string
	Angle      RefinedAngle
	BrandVoice BrandVoice
	StyleGuide string
}

type PolisherInput struct {
	Body       string
	BrandVoice BrandVoice
	StyleGuide string
}

type PolishedOutput struct {
	Body     string
	Hashtags []string
}

type TopicIdea struct {
	Title       string
	Hook        string
	Explanation string
}

type Generator interface {
	GenerateTopicIdeas(ctx context.Context, query string, exampleBodies []string) ([]TopicIdea, error)
	RefineTopic(ctx context.Context, input TopicRefinerInput) ([]RefinedAngle, error)
	WriteDraft(ctx context.Context, input DraftWriterInput) (string, error)
	PolishDraft(ctx context.Context, input PolisherInput) (*PolishedOutput, error)
	GenerateFromTopic(ctx context.Context, topic models.Topic, voice BrandVoice, styleGuide string) (*models.Draft, error)
}
