package models

import "time"

type Topic struct {
	ID        string    `bson:"_id,omitempty" json:"id"`
	UserID    string    `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Title     string    `bson:"title" json:"title"`
	URL       string    `bson:"url" json:"url"`
	Source    string    `bson:"source" json:"source"`
	Summary   string    `bson:"summary" json:"summary"`
	Keywords  []string  `bson:"keywords" json:"keywords"`
	ImageURL  string    `bson:"image_url,omitempty" json:"image_url,omitempty"`
	Used      bool      `bson:"used" json:"used"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
}
