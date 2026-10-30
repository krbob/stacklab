// Package backup coordinates native restic repositories and prepared recovery sets.
package backup

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound = errors.New("backup object not found")
	ErrInvalid  = errors.New("invalid backup configuration")
	ErrBusy     = errors.New("backup configuration is in use")
	ErrExpired  = errors.New("prepared recovery set expired; a new capture is required")
)

// Secrets is write-only API input. It is persisted separately in private files.
type Secrets struct {
	Password        string `json:"password,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	SessionToken    string `json:"session_token,omitempty"`
	SSHPrivateKey   string `json:"ssh_private_key,omitempty"`
	SSHKnownHosts   string `json:"ssh_known_hosts,omitempty"`
	RcloneConfig    string `json:"rclone_config,omitempty"`
}

type Destination struct {
	ID                        string        `json:"id"`
	Name                      string        `json:"name"`
	Kind                      string        `json:"kind"` // local, s3, sftp, rclone
	Repository                string        `json:"repository"`
	ExpectedFilesystem        string        `json:"expected_filesystem,omitempty"`
	Region                    string        `json:"region,omitempty"`
	RepositorySizeBytes       *int64        `json:"repository_size_bytes,omitempty"`
	CredentialsConfigured     bool          `json:"credentials_configured"`
	LastCheckAttemptAt        *time.Time    `json:"last_check_attempt_at,omitempty"`
	LastCheckAttemptPolicy    string        `json:"last_check_attempt_policy,omitempty"`
	LastCheckAttemptStatus    string        `json:"last_check_attempt_status,omitempty"`
	LastCheckAttemptError     string        `json:"last_check_attempt_error,omitempty"`
	MetadataCheck             CheckEvidence `json:"metadata_check"`
	DataCheck                 CheckEvidence `json:"data_check"`
	CheckHold                 *CheckHold    `json:"check_hold,omitempty"`
	MaintenanceReviewToken    string        `json:"maintenance_review_token,omitempty"`
	LastCheckAt               *time.Time    `json:"last_check_at,omitempty"`
	LastCheckPolicy           string        `json:"last_check_policy,omitempty"`
	CheckSchedule             Schedule      `json:"check_schedule"`
	CheckReadData             string        `json:"check_read_data"`
	PruneSchedule             Schedule      `json:"prune_schedule"`
	LastCheckScheduledAt      *time.Time    `json:"last_check_scheduled_at,omitempty"`
	LastPruneScheduledAt      *time.Time    `json:"last_prune_scheduled_at,omitempty"`
	LastPruneAt               *time.Time    `json:"last_prune_at,omitempty"`
	CheckStatus               string        `json:"check_status,omitempty"`
	CheckError                string        `json:"check_error,omitempty"`
	LastOperation             string        `json:"last_operation,omitempty"`
	LastOperationStatus       string        `json:"last_operation_status,omitempty"`
	LastOperationError        string        `json:"last_operation_error,omitempty"`
	LastOperationAt           *time.Time    `json:"last_operation_at,omitempty"`
	LastRestoreTestAt         *time.Time    `json:"last_restore_test_at,omitempty"`
	LastRestoreTestSnapshotID string        `json:"last_restore_test_snapshot_id,omitempty"`
	LastInventoryAt           *time.Time    `json:"last_inventory_at,omitempty"`
	LastInventoryAttemptAt    *time.Time    `json:"last_inventory_attempt_at,omitempty"`
	InventoryError            string        `json:"inventory_error,omitempty"`
	LastSuccessAt             *time.Time    `json:"last_success_at,omitempty"`
	Status                    string        `json:"status"`
	Error                     string        `json:"error,omitempty"`
}

// CheckEvidence describes durable proof, independently of the latest attempt.
// A successful sample cannot resolve a previously established data failure.
type CheckEvidence struct {
	Status    string     `json:"status"`
	Scope     string     `json:"scope"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// CheckHold preserves an unresolved repository check without claiming that
// objects are corrupt or discarding historical proof. Scope names the check
// required to resolve it: metadata, or all (also for an inconclusive sample).
type CheckHold struct {
	Scope     string     `json:"scope"`
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Error     string     `json:"error"`
}

type DestinationInput struct {
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	Repository         string   `json:"repository"`
	ExpectedFilesystem string   `json:"expected_filesystem,omitempty"`
	Region             string   `json:"region,omitempty"`
	Secrets            *Secrets `json:"secrets,omitempty"`
	CheckSchedule      Schedule `json:"check_schedule"`
	CheckReadData      string   `json:"check_read_data"`
	PruneSchedule      Schedule `json:"prune_schedule"`
}

type Schedule struct {
	Frequency     string `json:"frequency"`          // manual, interval, daily, weekly
	Time          string `json:"time,omitempty"`     // HH:MM
	Weekday       int    `json:"weekday,omitempty"`  // Sunday = 0
	Timezone      string `json:"timezone,omitempty"` // IANA zone, defaults host local
	IntervalHours int    `json:"interval_hours,omitempty"`
}

type Retention struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

type Assignment struct {
	DestinationID       string     `json:"destination_id"`
	Enabled             bool       `json:"enabled"`
	Schedule            Schedule   `json:"schedule"`
	Retention           Retention  `json:"retention"`
	LastScheduledAt     *time.Time `json:"last_scheduled_at,omitempty"`
	ScheduleActivatedAt *time.Time `json:"schedule_activated_at,omitempty"`
}

type Plan struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Enabled            bool         `json:"enabled"`
	Consistency        string       `json:"consistency"` // files, stop
	Sources            []Source     `json:"sources"`
	Destinations       []Assignment `json:"destinations"`
	StagingMaxBytes    int64        `json:"staging_max_bytes"`
	StagingTTLHours    int          `json:"staging_ttl_hours"`
	StopTimeoutSeconds int          `json:"stop_timeout_seconds"`
}

type CopyResult struct {
	DestinationID        string     `json:"destination_id"`
	State                string     `json:"state"`
	SnapshotID           string     `json:"snapshot_id,omitempty"`
	UploadedAt           *time.Time `json:"uploaded_at,omitempty"`
	Verification         string     `json:"verification"`
	VerifiedAt           *time.Time `json:"verified_at,omitempty"`
	Retention            string     `json:"retention"`
	RetentionError       string     `json:"retention_error,omitempty"`
	RetentionStartedAt   *time.Time `json:"retention_started_at,omitempty"`
	RetentionCompletedAt *time.Time `json:"retention_completed_at,omitempty"`
	Error                string     `json:"error,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	DurationSeconds      float64    `json:"duration_seconds,omitempty"`
	BytesProcessed       int64      `json:"bytes_processed,omitempty"`
	BytesTotal           int64      `json:"bytes_total,omitempty"`
	FilesProcessed       int64      `json:"files_processed,omitempty"`
	FilesTotal           int64      `json:"files_total,omitempty"`
	BytesAdded           int64      `json:"bytes_added,omitempty"`
}

type Run struct {
	ID                     string       `json:"id"`
	JobID                  string       `json:"job_id"`
	PlanID                 string       `json:"plan_id"`
	PlanName               string       `json:"plan_name"`
	State                  string       `json:"state"`
	CaptureState           string       `json:"capture_state"`
	Phase                  string       `json:"phase"`
	CompletedAt            *time.Time   `json:"completed_at,omitempty"`
	CaptureDurationSeconds float64      `json:"capture_duration_seconds,omitempty"`
	DurationSeconds        float64      `json:"duration_seconds,omitempty"`
	CapturedAt             *time.Time   `json:"captured_at,omitempty"`
	CreatedAt              time.Time    `json:"created_at"`
	ExpiresAt              time.Time    `json:"expires_at"`
	Copies                 []CopyResult `json:"copies"`
	Manifest               *Manifest    `json:"manifest,omitempty"`
	Error                  string       `json:"error,omitempty"`
	// Plan preserves capture selection when the operator edits a plan later;
	// maintenance uses current policy.
	Plan Plan `json:"-"`
}

type Snapshot struct {
	RecoveryStatus  string     `json:"recovery_status"`
	ClassifiedAt    *time.Time `json:"classified_at,omitempty"`
	RestoreTestedAt *time.Time `json:"restore_tested_at,omitempty"`
	ID              string     `json:"id"`
	ShortID         string     `json:"short_id"`
	Time            time.Time  `json:"time"`
	Paths           []string   `json:"paths"`
	Tags            []string   `json:"tags"`
	Hostname        string     `json:"hostname"`
}

type Overview struct {
	HelperConfigured bool             `json:"helper_configured"`
	HelperReady      bool             `json:"helper_ready"`
	HelperError      string           `json:"helper_error,omitempty"`
	RecoveryError    string           `json:"recovery_error,omitempty"`
	Destinations     []Destination    `json:"destinations"`
	Plans            []Plan           `json:"plans"`
	Runs             []Run            `json:"runs"`
	Protection       []PlanProtection `json:"protection"`
	ResticVersion    string           `json:"restic_version,omitempty"`
	ResticAvailable  bool             `json:"restic_available"`
	RcloneAvailable  bool             `json:"rclone_available"`
	StagingDirectory string           `json:"staging_directory"`
}

type Capturer interface {
	Prepare(context.Context, Plan, string, string) (Manifest, error)
	Recover(context.Context) error
}

type RunRequest struct {
	DestinationIDs []string `json:"destination_ids,omitempty"`
}
type CheckRequest struct {
	ReadData string `json:"read_data"`
} // metadata, all, subset

type RestoreRequest struct {
	RestoredDirectory string          `json:"restored_directory,omitempty"`
	RestoreID         string          `json:"restore_id,omitempty"`
	SourceIDs         []string        `json:"source_ids,omitempty"`
	Files             []FileSelection `json:"files,omitempty"`
	jobID             string
	Mappings          []RestoreMapping `json:"mappings,omitempty"`
	ReviewToken       string           `json:"review_token,omitempty"`
	ResumeServices    bool             `json:"resume_services,omitempty"`
	SnapshotID        string           `json:"snapshot_id"`
	Target            string           `json:"target,omitempty"`
	Includes          []string         `json:"includes,omitempty"`
	Test              bool             `json:"test,omitempty"`
}

type RestoreResult struct {
	RecoveryStatus    string          `json:"recovery_status"`
	ClassifiedAt      *time.Time      `json:"classified_at,omitempty"`
	Managed           bool            `json:"managed"`
	Available         bool            `json:"available"`
	UnavailableReason string          `json:"unavailable_reason,omitempty"`
	ID                string          `json:"id"`
	JobID             string          `json:"job_id,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	SelectedFiles     []FileSelection `json:"selected_files,omitempty"`
	SourceIDs         []string        `json:"source_ids,omitempty"`
	DestinationID     string          `json:"destination_id"`
	SnapshotID        string          `json:"snapshot_id"`
	Target            string          `json:"target"`
	Test              bool            `json:"test"`
	Manifest          *Manifest       `json:"manifest,omitempty"`
}
