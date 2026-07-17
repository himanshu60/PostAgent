package models

import "time"

type PublishedPost struct {
	ID              string    `bson:"_id,omitempty" json:"id"`
	DraftID         string    `bson:"draft_id" json:"draft_id"`
	LinkedInPostID  string    `bson:"linkedin_post_id" json:"linkedin_post_id"`
	PostURL         string    `bson:"post_url" json:"post_url"`
	PostedAt        time.Time `bson:"posted_at" json:"posted_at"`
	Likes           int       `bson:"likes" json:"likes"`
	Comments        int       `bson:"comments" json:"comments"`
	Impressions     int       `bson:"impressions" json:"impressions"`
	LastAnalyticsFetch *time.Time `bson:"last_analytics_fetch,omitempty" json:"last_analytics_fetch,omitempty"`
}
