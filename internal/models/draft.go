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
	// Card describes a handwritten-notes image. It is rendered on demand for
	// the preview and again at publish time (same seed → identical image), so
	// no image hosting is needed. Nil when the image comes from elsewhere.
	Card *NoteCard `bson:"card,omitempty" json:"card,omitempty"`
}

// NoteCard is the content + look of a handwritten sketchnote post image: a
// hand-drawn page with a title, highlighted subtitle, a first-person note, a
// doodle, a labelled diagram, annotations, the lesson, a closing question and
// hashtags. Text may mark a key phrase with ==double equals== to highlight it.
// Every field except Headline is optional; the layout adapts to what's there.
type NoteCard struct {
	Headline  string   `bson:"headline" json:"headline"`
	Subtitle  string   `bson:"subtitle,omitempty" json:"subtitle,omitempty"`
	Intro     string   `bson:"intro,omitempty" json:"intro,omitempty"`           // first-person note
	Icon      string   `bson:"icon,omitempty" json:"icon,omitempty"`             // main doodle, e.g. "robot"
	IconLabel string   `bson:"icon_label,omitempty" json:"icon_label,omitempty"` // caption beside the doodle
	Callout   string   `bson:"callout,omitempty" json:"callout,omitempty"`       // short question/thought under it
	Keyword   string   `bson:"keyword,omitempty" json:"keyword,omitempty"`       // boxed star term
	BoxTitle  string   `bson:"box_title,omitempty" json:"box_title,omitempty"`   // diagram panel heading
	Points    []string `bson:"points,omitempty" json:"points,omitempty"`         // diagram items
	Notes     []string `bson:"notes,omitempty" json:"notes,omitempty"`           // annotations
	Footer    string   `bson:"footer,omitempty" json:"footer,omitempty"`         // "The Lesson:"
	Question  string   `bson:"question,omitempty" json:"question,omitempty"`
	Hashtags  []string `bson:"hashtags,omitempty" json:"hashtags,omitempty"`
	Style     string   `bson:"style" json:"style"` // paper | notebook | grid | whiteboard | chalkboard
	Seed      int64    `bson:"seed" json:"seed"`   // drives the hand-drawn jitter deterministically
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
	// Format is the post shape used (e.g. "story", "listicle"), recorded so the
	// next posts can avoid repeating the same shape.
	Format string `bson:"format,omitempty" json:"format,omitempty"`
	// HideTitle drops the bold title line when publishing. Conversational
	// formats (stories, hot takes) read more human without a headline.
	// Defaults to false so existing drafts keep their title.
	HideTitle bool      `bson:"hide_title,omitempty" json:"hide_title,omitempty"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}
