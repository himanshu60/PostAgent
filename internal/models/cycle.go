package models

import "time"

type CycleStatus string

const (
	CycleStatusRunning   CycleStatus = "running"
	CycleStatusCompleted CycleStatus = "completed"
	CycleStatusSkipped   CycleStatus = "skipped"
)

type Cycle struct {
	ID        string      `bson:"_id,omitempty" json:"id"`
	UserID    string      `bson:"user_id,omitempty" json:"user_id,omitempty"`
	Status    CycleStatus `bson:"status" json:"status"`
	DraftIDs  []string    `bson:"draft_ids" json:"draft_ids"`
	StartedAt time.Time   `bson:"started_at" json:"started_at"`
	EndedAt   *time.Time  `bson:"ended_at,omitempty" json:"ended_at,omitempty"`
	// Trigger identifies what started this cycle (e.g. "daily_auto" for the
	// once-a-day auto-generate job, "manual" for the Run Cycle button/CLI).
	// Lets the daily job check "did today's run already happen?" without
	// confusing itself with manually-triggered cycles.
	Trigger string `bson:"trigger,omitempty" json:"trigger,omitempty"`
}
