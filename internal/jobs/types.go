package jobs

import "time"

type ActiveJobsResponse struct {
	Items   []ActiveJobItem `json:"items"`
	Summary ActiveJobCounts `json:"summary"`
}

type ActiveJobCounts struct {
	ActiveCount          int `json:"active_count"`
	RunningCount         int `json:"running_count"`
	QueuedCount          int `json:"queued_count"`
	CancelRequestedCount int `json:"cancel_requested_count"`
}

type ActiveJobItem struct {
	ID          string          `json:"id"`
	StackID     *string         `json:"stack_id"`
	Action      string          `json:"action"`
	State       string          `json:"state"`
	RequestedAt time.Time       `json:"requested_at"`
	StartedAt   *time.Time      `json:"started_at,omitempty"`
	Workflow    *ActiveWorkflow `json:"workflow,omitempty"`
	CurrentStep *ActiveJobStep  `json:"current_step,omitempty"`
	LatestEvent *ActiveJobEvent `json:"latest_event,omitempty"`
}

type ActiveWorkflow struct {
	Steps []ActiveWorkflowStep `json:"steps"`
}

type ActiveWorkflowStep struct {
	Action        string `json:"action"`
	State         string `json:"state"`
	TargetStackID string `json:"target_stack_id,omitempty"`
}

type ActiveJobStep struct {
	Index         int    `json:"index"`
	Total         int    `json:"total"`
	Action        string `json:"action"`
	TargetStackID string `json:"target_stack_id,omitempty"`
}

type ActiveJobEvent struct {
	Event     string         `json:"event"`
	Message   string         `json:"message,omitempty"`
	Data      string         `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Step      *ActiveJobStep `json:"step,omitempty"`
}
