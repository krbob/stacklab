package workspacerepair

import "stacklab/internal/fsmeta"

type Capability struct {
	Supported bool    `json:"supported"`
	Reason    *string `json:"reason,omitempty"`
	Recursive bool    `json:"recursive"`
}

type Result struct {
	ChangedItems            int                `json:"changed_items"`
	Warnings                []string           `json:"warnings,omitempty"`
	TargetPermissionsBefore fsmeta.Permissions `json:"target_permissions_before"`
	TargetPermissionsAfter  fsmeta.Permissions `json:"target_permissions_after"`
}
