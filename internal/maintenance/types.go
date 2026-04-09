package maintenance

import "time"

type ImageUsage string

const (
	ImageUsageAll    ImageUsage = "all"
	ImageUsageUsed   ImageUsage = "used"
	ImageUsageUnused ImageUsage = "unused"
)

type ImageOrigin string

const (
	ImageOriginAll          ImageOrigin = "all"
	ImageOriginStackManaged ImageOrigin = "stack_managed"
	ImageOriginExternal     ImageOrigin = "external"
)

type ImageSource string

const (
	ImageSourceStackManaged ImageSource = "stack_managed"
	ImageSourceExternal     ImageSource = "external"
)

type ImagesQuery struct {
	Search          string
	Usage           ImageUsage
	Origin          ImageOrigin
	ManagedStackIDs []string
}

type ImagesResponse struct {
	Items []ImageItem `json:"items"`
}

type StackServiceUsage struct {
	StackID      string   `json:"stack_id"`
	ServiceNames []string `json:"service_names"`
}

type ImageItem struct {
	ID              string              `json:"id"`
	Repository      string              `json:"repository"`
	Tag             string              `json:"tag"`
	Reference       string              `json:"reference"`
	SizeBytes       int64               `json:"size_bytes"`
	CreatedAt       time.Time           `json:"created_at"`
	ContainersUsing int                 `json:"containers_using"`
	StacksUsing     []StackServiceUsage `json:"stacks_using"`
	IsDangling      bool                `json:"is_dangling"`
	IsUnused        bool                `json:"is_unused"`
	Source          ImageSource         `json:"source"`
}

type NetworksQuery struct {
	Search          string
	Usage           ImageUsage
	Origin          ImageOrigin
	ManagedStackIDs []string
}

type NetworkSource string

const (
	NetworkSourceStackManaged NetworkSource = "stack_managed"
	NetworkSourceExternal     NetworkSource = "external"
)

type NetworksResponse struct {
	Items []NetworkItem `json:"items"`
}

type NetworkItem struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Driver          string              `json:"driver"`
	Scope           string              `json:"scope"`
	Internal        bool                `json:"internal"`
	Attachable      bool                `json:"attachable"`
	Ingress         bool                `json:"ingress"`
	ContainersUsing int                 `json:"containers_using"`
	StacksUsing     []StackServiceUsage `json:"stacks_using"`
	IsUnused        bool                `json:"is_unused"`
	Source          NetworkSource       `json:"source"`
}

type CreateNetworkRequest struct {
	Name string `json:"name"`
}

type CreateNetworkResponse struct {
	Created bool   `json:"created"`
	Name    string `json:"name"`
}

type DeleteNetworkResponse struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

type VolumesQuery struct {
	Search          string
	Usage           ImageUsage
	Origin          ImageOrigin
	ManagedStackIDs []string
}

type VolumeSource string

const (
	VolumeSourceStackManaged VolumeSource = "stack_managed"
	VolumeSourceExternal     VolumeSource = "external"
)

type VolumesResponse struct {
	Items []VolumeItem `json:"items"`
}

type VolumeItem struct {
	Name            string              `json:"name"`
	Driver          string              `json:"driver"`
	Mountpoint      string              `json:"mountpoint"`
	Scope           string              `json:"scope"`
	OptionsCount    int                 `json:"options_count"`
	ContainersUsing int                 `json:"containers_using"`
	StacksUsing     []StackServiceUsage `json:"stacks_using"`
	IsUnused        bool                `json:"is_unused"`
	Source          VolumeSource        `json:"source"`
}

type CreateVolumeRequest struct {
	Name string `json:"name"`
}

type CreateVolumeResponse struct {
	Created bool   `json:"created"`
	Name    string `json:"name"`
}

type DeleteVolumeResponse struct {
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

type PrunePreviewQuery struct {
	Images            bool
	BuildCache        bool
	StoppedContainers bool
	Volumes           bool
	ManagedStackIDs   []string
}

type PrunePreviewResponse struct {
	Preview PrunePreview `json:"preview"`
}

type PrunePreview struct {
	Images                PruneCategoryPreview `json:"images"`
	BuildCache            PruneCategoryPreview `json:"build_cache"`
	StoppedContainers     PruneCategoryPreview `json:"stopped_containers"`
	Volumes               PruneCategoryPreview `json:"volumes"`
	TotalReclaimableBytes int64                `json:"total_reclaimable_bytes"`
}

type PruneCategoryPreview struct {
	Count            int                `json:"count"`
	ReclaimableBytes int64              `json:"reclaimable_bytes"`
	Items            []PrunePreviewItem `json:"items,omitempty"`
}

type PrunePreviewItem struct {
	Reference string `json:"reference"`
	SizeBytes int64  `json:"size_bytes"`
	Reason    string `json:"reason"`
}

type PruneScope struct {
	Images            bool `json:"images"`
	BuildCache        bool `json:"build_cache"`
	StoppedContainers bool `json:"stopped_containers"`
	Volumes           bool `json:"volumes"`
}
