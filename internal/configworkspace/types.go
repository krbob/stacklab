package configworkspace

import (
	"time"

	"stacklab/internal/fsmeta"
	"stacklab/internal/workspacerepair"
)

type EntryType string

const (
	EntryTypeDirectory   EntryType = "directory"
	EntryTypeTextFile    EntryType = "text_file"
	EntryTypeBinaryFile  EntryType = "binary_file"
	EntryTypeUnknownFile EntryType = "unknown_file"
)

type TreeResponse struct {
	WorkspaceRoot string      `json:"workspace_root"`
	CurrentPath   string      `json:"current_path"`
	ParentPath    *string     `json:"parent_path"`
	Items         []TreeEntry `json:"items"`
}

type TreeEntry struct {
	Name        string             `json:"name"`
	Path        string             `json:"path"`
	Type        EntryType          `json:"type"`
	SizeBytes   int64              `json:"size_bytes"`
	ModifiedAt  time.Time          `json:"modified_at"`
	StackID     *string            `json:"stack_id"`
	Permissions fsmeta.Permissions `json:"permissions"`
}

type FileResponse struct {
	Path             string                     `json:"path"`
	Name             string                     `json:"name"`
	Type             EntryType                  `json:"type"`
	StackID          *string                    `json:"stack_id"`
	Content          *string                    `json:"content"`
	Encoding         *string                    `json:"encoding"`
	SizeBytes        int64                      `json:"size_bytes"`
	ModifiedAt       time.Time                  `json:"modified_at"`
	Readable         bool                       `json:"readable"`
	Writable         bool                       `json:"writable"`
	BlockedReason    *string                    `json:"blocked_reason"`
	Permissions      fsmeta.Permissions         `json:"permissions"`
	RepairCapability workspacerepair.Capability `json:"repair_capability"`
}

type SaveFileRequest struct {
	Path                    string `json:"path"`
	Content                 string `json:"content"`
	CreateParentDirectories bool   `json:"create_parent_directories"`
}

type SaveFileResponse struct {
	Saved       bool      `json:"saved"`
	Path        string    `json:"path"`
	ModifiedAt  time.Time `json:"modified_at"`
	AuditAction string    `json:"audit_action"`
}

type RepairPermissionsRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

type RepairPermissionsResponse struct {
	Repaired                bool                       `json:"repaired"`
	Path                    string                     `json:"path"`
	Recursive               bool                       `json:"recursive"`
	ChangedItems            int                        `json:"changed_items"`
	Warnings                []string                   `json:"warnings,omitempty"`
	TargetPermissionsBefore fsmeta.Permissions         `json:"target_permissions_before"`
	TargetPermissionsAfter  fsmeta.Permissions         `json:"target_permissions_after"`
	AuditAction             string                     `json:"audit_action"`
	RepairCapability        workspacerepair.Capability `json:"repair_capability"`
}
