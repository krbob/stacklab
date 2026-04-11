package maintenancejobs

import "stacklab/internal/maintenance"

type UpdateTarget struct {
	Mode     string   `json:"mode"`
	StackIDs []string `json:"stack_ids,omitempty"`
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

type PruneRequest struct {
	Scope       maintenance.PruneScope `json:"scope"`
	Trigger     string                 `json:"trigger,omitempty"`
	ScheduleKey string                 `json:"schedule_key,omitempty"`
}
