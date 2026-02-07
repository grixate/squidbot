package cron

import "time"

type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"
	ScheduleEvery ScheduleKind = "every"
	ScheduleCron  ScheduleKind = "cron"
)

type Job struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Enabled   bool        `json:"enabled"`
	Schedule  JobSchedule `json:"schedule"`
	Payload   JobPayload  `json:"payload"`
	State     JobState    `json:"state"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	Version   int         `json:"version"`
}

type JobSchedule struct {
	Kind  ScheduleKind `json:"kind"`
	At    *time.Time   `json:"at,omitempty"`
	Every int64        `json:"every_ms,omitempty"`
	Expr  string       `json:"expr,omitempty"`
	TZ    string       `json:"tz,omitempty"`
}

type JobPayload struct {
	Message string `json:"message"`
	Deliver bool   `json:"deliver"`
	Channel string `json:"channel,omitempty"`
	To      string `json:"to,omitempty"`
}

type JobState struct {
	NextRunAt  *time.Time `json:"next_run_at,omitempty"`
	LastRunAt  *time.Time `json:"last_run_at,omitempty"`
	LastStatus string     `json:"last_status,omitempty"`
	LastError  string     `json:"last_error,omitempty"`
}
