package hostinfo

import "time"

type OverviewResponse struct {
	Host      HostMeta      `json:"host"`
	Stacklab  StacklabMeta  `json:"stacklab"`
	Docker    DockerMeta    `json:"docker"`
	Resources ResourceUsage `json:"resources"`
}

type HostMeta struct {
	Hostname      string `json:"hostname"`
	OSName        string `json:"os_name"`
	KernelVersion string `json:"kernel_version"`
	Architecture  string `json:"architecture"`
	UptimeSeconds int64  `json:"uptime_seconds"`
}

type StacklabMeta struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	StartedAt time.Time `json:"started_at"`
}

type DockerMeta struct {
	EngineVersion  string `json:"engine_version"`
	ComposeVersion string `json:"compose_version"`
}

type ResourceUsage struct {
	CPU    CPUUsage    `json:"cpu"`
	Memory MemoryUsage `json:"memory"`
	Disk   DiskUsage   `json:"disk"`
}

type CPUUsage struct {
	CoreCount    int       `json:"core_count"`
	LoadAverage  []float64 `json:"load_average"`
	UsagePercent float64   `json:"usage_percent"`
}

type MemoryUsage struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

type DiskUsage struct {
	Path           string  `json:"path"`
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

type LogsQuery struct {
	Limit  int
	Cursor string
	Level  string
	Search string
}

type StacklabLogsResponse struct {
	Items      []StacklabLogEntry `json:"items"`
	NextCursor string             `json:"next_cursor,omitempty"`
	HasMore    bool               `json:"has_more"`
}

type StacklabLogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
	Cursor    string    `json:"cursor"`
}
