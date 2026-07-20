package models

import "time"

// User is a tenant. Identity + LinkedIn posting tokens come from a single
// "Sign in with LinkedIn" OAuth flow. The user's LinkedIn `sub` (person id) is
// used as the primary id, so every user maps 1:1 to a LinkedIn account.
type User struct {
	ID             string    `bson:"_id,omitempty" json:"id"` // == LinkedIn sub (person id)
	Name           string    `bson:"name" json:"name"`
	Email          string    `bson:"email,omitempty" json:"email,omitempty"`
	Picture        string    `bson:"picture,omitempty" json:"picture,omitempty"`
	AccessToken    string    `bson:"access_token" json:"-"`
	RefreshToken   string    `bson:"refresh_token" json:"-"`
	TokenExpiresAt time.Time `bson:"token_expires_at,omitempty" json:"-"`
	// Per-user posting settings (default off/global so a new user never
	// auto-publishes until they opt in).
	AutoPublish bool   `bson:"auto_publish" json:"auto_publish"`
	PublishDays string `bson:"publish_days,omitempty" json:"publish_days,omitempty"` // e.g. "Mon,Wed,Fri"
	// Role is the user's designation (e.g. "Backend Engineer"). Interests are the
	// topics they want posts about (e.g. ["Node.js","System Design","DSA"]). When
	// set, auto-generated posts are drawn from these instead of generic trends.
	Role        string    `bson:"role,omitempty" json:"role,omitempty"`
	Interests   []string  `bson:"interests,omitempty" json:"interests,omitempty"`
	CreatedAt   time.Time `bson:"created_at" json:"created_at"`
	LastLoginAt time.Time `bson:"last_login_at" json:"last_login_at"`
}

// Session is a server-side login session keyed by an opaque random token that
// lives in a secure, http-only cookie.
type Session struct {
	Token     string    `bson:"_id" json:"-"`
	UserID    string    `bson:"user_id" json:"user_id"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
}
