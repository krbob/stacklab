package scheduler

import (
	"time"

	"stacklab/internal/maintenance"
	"stacklab/internal/maintenancejobs"
)

type Frequency string

const (
	FrequencyDaily  Frequency = "daily"
	FrequencyWeekly Frequency = "weekly"
)

type Weekday string

const (
	WeekdayMonday    Weekday = "mon"
	WeekdayTuesday   Weekday = "tue"
	WeekdayWednesday Weekday = "wed"
	WeekdayThursday  Weekday = "thu"
	WeekdayFriday    Weekday = "fri"
	WeekdaySaturday  Weekday = "sat"
	WeekdaySunday    Weekday = "sun"
)

type UpdateScheduleConfig struct {
	Enabled   bool                          `json:"enabled"`
	Frequency Frequency                     `json:"frequency"`
	Time      string                        `json:"time"`
	Weekdays  []Weekday                     `json:"weekdays,omitempty"`
	Target    maintenancejobs.UpdateTarget  `json:"target"`
	Options   maintenancejobs.UpdateOptions `json:"options"`
}

type PruneScheduleConfig struct {
	Enabled   bool                   `json:"enabled"`
	Frequency Frequency              `json:"frequency"`
	Time      string                 `json:"time"`
	Weekdays  []Weekday              `json:"weekdays,omitempty"`
	Scope     maintenance.PruneScope `json:"scope"`
}

type Settings struct {
	Update UpdateScheduleConfig `json:"update"`
	Prune  PruneScheduleConfig  `json:"prune"`
}

type ScheduleStatus struct {
	NextRunAt        *time.Time `json:"next_run_at,omitempty"`
	LastTriggeredAt  *time.Time `json:"last_triggered_at,omitempty"`
	LastScheduledFor *time.Time `json:"last_scheduled_for,omitempty"`
	LastResult       string     `json:"last_result,omitempty"`
	LastMessage      string     `json:"last_message,omitempty"`
	LastJobID        *string    `json:"last_job_id,omitempty"`
}

type SettingsResponse struct {
	Timezone string                `json:"timezone"`
	Update   ScheduledUpdatePolicy `json:"update"`
	Prune    ScheduledPrunePolicy  `json:"prune"`
}

type ScheduledUpdatePolicy struct {
	UpdateScheduleConfig
	Status ScheduleStatus `json:"status"`
}

type ScheduledPrunePolicy struct {
	PruneScheduleConfig
	Status ScheduleStatus `json:"status"`
}

type UpdateSettingsRequest struct {
	Update UpdateScheduleConfig `json:"update"`
	Prune  PruneScheduleConfig  `json:"prune"`
}

type runtimeState struct {
	Update scheduleRuntimeState `json:"update"`
	Prune  scheduleRuntimeState `json:"prune"`
}

type scheduleRuntimeState struct {
	LastTriggeredAt  *time.Time `json:"last_triggered_at,omitempty"`
	LastScheduledFor *time.Time `json:"last_scheduled_for,omitempty"`
	LastResult       string     `json:"last_result,omitempty"`
	LastMessage      string     `json:"last_message,omitempty"`
	LastJobID        string     `json:"last_job_id,omitempty"`
}
