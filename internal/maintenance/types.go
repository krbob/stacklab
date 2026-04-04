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

type ImageItem struct {
	ID              string            `json:"id"`
	Repository      string            `json:"repository"`
	Tag             string            `json:"tag"`
	Reference       string            `json:"reference"`
	SizeBytes       int64             `json:"size_bytes"`
	CreatedAt       time.Time         `json:"created_at"`
	ContainersUsing int               `json:"containers_using"`
	StacksUsing     []ImageStackUsage `json:"stacks_using"`
	IsDangling      bool              `json:"is_dangling"`
	IsUnused        bool              `json:"is_unused"`
	Source          ImageSource       `json:"source"`
}

type ImageStackUsage struct {
	StackID      string   `json:"stack_id"`
	ServiceNames []string `json:"service_names"`
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
