package gitworkspace

type Scope string

const (
	ScopeStacks Scope = "stacks"
	ScopeConfig Scope = "config"
)

type FileStatus string

const (
	FileStatusModified   FileStatus = "modified"
	FileStatusAdded      FileStatus = "added"
	FileStatusDeleted    FileStatus = "deleted"
	FileStatusRenamed    FileStatus = "renamed"
	FileStatusUntracked  FileStatus = "untracked"
	FileStatusConflicted FileStatus = "conflicted"
)

type StatusResponse struct {
	Available    bool         `json:"available"`
	RepoRoot     string       `json:"repo_root"`
	ManagedRoots []string     `json:"managed_roots"`
	Branch       string       `json:"branch,omitempty"`
	HeadCommit   string       `json:"head_commit,omitempty"`
	HasUpstream  bool         `json:"has_upstream,omitempty"`
	UpstreamName string       `json:"upstream_name,omitempty"`
	AheadCount   int          `json:"ahead_count,omitempty"`
	BehindCount  int          `json:"behind_count,omitempty"`
	Clean        bool         `json:"clean,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	Items        []StatusItem `json:"items,omitempty"`
}

type StatusItem struct {
	Path    string     `json:"path"`
	Scope   Scope      `json:"scope"`
	StackID *string    `json:"stack_id"`
	Status  FileStatus `json:"status"`
	OldPath *string    `json:"old_path"`
}

type DiffResponse struct {
	Available bool       `json:"available"`
	Path      string     `json:"path"`
	Scope     Scope      `json:"scope"`
	StackID   *string    `json:"stack_id"`
	Status    FileStatus `json:"status"`
	OldPath   *string    `json:"old_path"`
	IsBinary  bool       `json:"is_binary"`
	Diff      *string    `json:"diff"`
	Truncated bool       `json:"truncated"`
}

type CommitRequest struct {
	Message string   `json:"message"`
	Paths   []string `json:"paths"`
}

type CommitResponse struct {
	Committed        bool     `json:"committed"`
	Commit           string   `json:"commit"`
	Summary          string   `json:"summary"`
	Paths            []string `json:"paths"`
	RemainingChanges int      `json:"remaining_changes"`
}

type PushResponse struct {
	Pushed       bool   `json:"pushed"`
	Remote       string `json:"remote"`
	Branch       string `json:"branch"`
	UpstreamName string `json:"upstream_name"`
	HeadCommit   string `json:"head_commit"`
	AheadCount   int    `json:"ahead_count"`
	BehindCount  int    `json:"behind_count"`
}
