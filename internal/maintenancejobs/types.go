package maintenancejobs

import (
	"stacklab/internal/maintenance"
	"stacklab/internal/store"
)

type UpdateTarget struct {
	Mode             string              `json:"mode"`
	StackIDs         []string            `json:"stack_ids,omitempty"`
	ExcludedServices map[string][]string `json:"excluded_services,omitempty"`
}

type UpdateOptions struct {
	PullImages     bool `json:"pull_images"`
	BuildImages    bool `json:"build_images"`
	RemoveOrphans  bool `json:"remove_orphans"`
	PruneAfter     bool `json:"prune_after"`
	IncludeVolumes bool `json:"include_volumes"`
}

type UpdateRequest struct {
	Target      UpdateTarget  `json:"target"`
	Options     UpdateOptions `json:"options"`
	Trigger     string        `json:"trigger,omitempty"`
	ScheduleKey string        `json:"schedule_key,omitempty"`
}

type UpdateRun struct {
	Request        UpdateRequest
	TargetStackIDs []string
	Workflow       []store.JobWorkflowStep
}

type PruneRequest struct {
	Scope       maintenance.PruneScope `json:"scope"`
	Trigger     string                 `json:"trigger,omitempty"`
	ScheduleKey string                 `json:"schedule_key,omitempty"`
}

type PruneRun struct {
	Request  PruneRequest
	Workflow []store.JobWorkflowStep
}
