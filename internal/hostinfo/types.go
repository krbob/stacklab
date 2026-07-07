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

type MetricsResponse struct {
	SampleIntervalSeconds           int                `json:"sample_interval_seconds"`
	BackgroundSampleIntervalSeconds int                `json:"background_sample_interval_seconds"`
	ActiveSampleIntervalSeconds     int                `json:"active_sample_interval_seconds"`
	HistoryWindowSeconds            int                `json:"history_window_seconds"`
	Current                         *HostMetricSample  `json:"current"`
	History                         []HostMetricSample `json:"history"`
}

type MetricsQuery struct {
	Since *time.Time
}

type HostMetricSample struct {
	SampledAt    time.Time         `json:"sampled_at"`
	CPU          CPUUsage          `json:"cpu"`
	Memory       MemoryUsage       `json:"memory"`
	Swap         SwapUsage         `json:"swap"`
	Temperatures TemperatureUsage  `json:"temperatures"`
	Filesystems  []FilesystemUsage `json:"filesystems"`
	DiskIO       DiskIOUsage       `json:"disk_io"`
	Network      NetworkUsage      `json:"network"`
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

type SwapUsage struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

type TemperatureUsage struct {
	CPUCelsius *float64            `json:"cpu_celsius"`
	CPUSensor  *TemperatureSensor  `json:"cpu_sensor,omitempty"`
	Sensors    []TemperatureSensor `json:"sensors"`
}

type TemperatureSensor struct {
	Name               string  `json:"name"`
	Label              string  `json:"label"`
	TemperatureCelsius float64 `json:"temperature_celsius"`
}

type DiskUsage struct {
	Path           string  `json:"path"`
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
}

type FilesystemUsage struct {
	MountPoint     string  `json:"mount_point"`
	Device         string  `json:"device"`
	FSType         string  `json:"fs_type"`
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	Primary        bool    `json:"primary"`
}

type DiskIOUsage struct {
	TotalReadBytesPerSec  float64             `json:"total_read_bytes_per_sec"`
	TotalWriteBytesPerSec float64             `json:"total_write_bytes_per_sec"`
	Devices               []DiskIODeviceUsage `json:"devices"`
}

type DiskIODeviceUsage struct {
	Name             string  `json:"name"`
	ReadBytes        uint64  `json:"read_bytes"`
	WriteBytes       uint64  `json:"write_bytes"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
}

type NetworkUsage struct {
	TotalRXBytesPerSec float64                 `json:"total_rx_bytes_per_sec"`
	TotalTXBytesPerSec float64                 `json:"total_tx_bytes_per_sec"`
	PublicIP           string                  `json:"public_ip,omitempty"`
	Interfaces         []NetworkInterfaceUsage `json:"interfaces"`
}

type NetworkInterfaceUsage struct {
	Name          string  `json:"name"`
	RXBytes       uint64  `json:"rx_bytes"`
	TXBytes       uint64  `json:"tx_bytes"`
	RXBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TXBytesPerSec float64 `json:"tx_bytes_per_sec"`
}

type LogsQuery struct {
	Limit             int
	Cursor            string
	Level             string
	Search            string
	IncludeHTTPAccess bool
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
