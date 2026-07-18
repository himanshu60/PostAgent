package models

import "time"

type DraftStatus string

const (
	DraftStatusDraft     DraftStatus = "draft"
	DraftStatusSelected  DraftStatus = "selected"
	DraftStatusApproved  DraftStatus = "approved"
	DraftStatusRejected  DraftStatus = "rejected"
	DraftStatusPublished DraftStatus = "published"
)

type MediaType string

const (
	MediaTypeImage MediaType = "image"
	MediaTypePDF   MediaType = "pdf"
	MediaTypeNone  MediaType = "none"
)

type Media struct {
	Type             MediaType `bson:"type" json:"type"`
	SourceURL        string    `bson:"source_url,omitempty" json:"source_url,omitempty"`
	LocalPath        string    `bson:"local_path,omitempty" json:"local_path,omitempty"`
	LinkedInAssetURN string    `bson:"linkedin_asset_urn,omitempty" json:"linkedin_asset_urn,omitempty"`
	AltText          string    `bson:"alt_text,omitempty" json:"alt_text,omitempty"`
	// Prompt is the image-generation prompt, used to (re)generate the image at
	// publish time (e.g. via OpenAI) when a live generator is configured.
	Prompt string `bson:"prompt,omitempty" json:"prompt,omitempty"`
}

type Draft struct {
	ID           string      `bson:"_id,omitempty" json:"id"`
	UserID       string      `bson:"user_id,omitempty" json:"user_id,omitempty"`
	TopicID      string      `bson:"topic_id" json:"topic_id"`
	Title        string      `bson:"title,omitempty" json:"title,omitempty"`
	Body         string      `bson:"body" json:"body"`
	Hashtags     []string    `bson:"hashtags" json:"hashtags"`
	Status       DraftStatus `bson:"status" json:"status"`
	Media        *Media      `bson:"media,omitempty" json:"media,omitempty"`
	ReviewNotes  string      `bson:"review_notes,omitempty" json:"review_notes,omitempty"`
	ScheduledFor *time.Time  `bson:"scheduled_for,omitempty" json:"scheduled_for,omitempty"`
	CycleID      string      `bson:"cycle_id" json:"cycle_id"`
	CreatedAt    time.Time   `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time   `bson:"updated_at" json:"updated_at"`
}
