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
	ID                    string     `json:"id"`
	Name                  string     `json:"name"`
	Kind                  string     `json:"kind"` // local, s3, sftp, rclone
	Repository            string     `json:"repository"`
	ExpectedFilesystem    string     `json:"expected_filesystem,omitempty"`
	Region                string     `json:"region,omitempty"`
	RepositorySizeBytes   *int64     `json:"repository_size_bytes,omitempty"`
	CredentialsConfigured bool       `json:"credentials_configured"`
	LastCheckAt           *time.Time `json:"last_check_at,omitempty"`
	LastCheckPolicy       string     `json:"last_check_policy,omitempty"`
	LastRestoreTestAt     *time.Time `json:"last_restore_test_at,omitempty"`
	LastSuccessAt         *time.Time `json:"last_success_at,omitempty"`
	Status                string     `json:"status"`
	Error                 string     `json:"error,omitempty"`
}

type DestinationInput struct {
	Name               string   `json:"name"`
	Kind               string   `json:"kind"`
	Repository         string   `json:"repository"`
	ExpectedFilesystem string   `json:"expected_filesystem,omitempty"`
	Region             string   `json:"region,omitempty"`
	Secrets            *Secrets `json:"secrets,omitempty"`
}

type Schedule struct {
	Frequency string `json:"frequency"`          // manual, daily, weekly
	Time      string `json:"time,omitempty"`     // HH:MM
	Weekday   int    `json:"weekday,omitempty"`  // Sunday = 0
	Timezone  string `json:"timezone,omitempty"` // IANA zone, defaults host local
}

type Retention struct {
	Daily   int `json:"daily"`
	Weekly  int `json:"weekly"`
	Monthly int `json:"monthly"`
}

type Assignment struct {
	DestinationID   string     `json:"destination_id"`
	Enabled         bool       `json:"enabled"`
	Schedule        Schedule   `json:"schedule"`
	Retention       Retention  `json:"retention"`
	LastScheduledAt *time.Time `json:"last_scheduled_at,omitempty"`
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
	DestinationID string     `json:"destination_id"`
	State         string     `json:"state"`
	SnapshotID    string     `json:"snapshot_id,omitempty"`
	UploadedAt    *time.Time `json:"uploaded_at,omitempty"`
	Verification  string     `json:"verification"`
	Retention     string     `json:"retention"`
	Error         string     `json:"error,omitempty"`
}

type Run struct {
	ID           string       `json:"id"`
	JobID        string       `json:"job_id"`
	PlanID       string       `json:"plan_id"`
	PlanName     string       `json:"plan_name"`
	State        string       `json:"state"`
	CaptureState string       `json:"capture_state"`
	CapturedAt   *time.Time   `json:"captured_at,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
	ExpiresAt    time.Time    `json:"expires_at"`
	Copies       []CopyResult `json:"copies"`
	Manifest     *Manifest    `json:"manifest,omitempty"`
	Error        string       `json:"error,omitempty"`
	// Plan preserves selection and retention when the operator edits a plan later.
	Plan Plan `json:"-"`
}

type Snapshot struct {
	ID       string    `json:"id"`
	ShortID  string    `json:"short_id"`
	Time     time.Time `json:"time"`
	Paths    []string  `json:"paths"`
	Tags     []string  `json:"tags"`
	Hostname string    `json:"hostname"`
}

type Overview struct {
	Destinations     []Destination `json:"destinations"`
	Plans            []Plan        `json:"plans"`
	Runs             []Run         `json:"runs"`
	ResticAvailable  bool          `json:"restic_available"`
	RcloneAvailable  bool          `json:"rclone_available"`
	StagingDirectory string        `json:"staging_directory"`
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
	RestoredDirectory string           `json:"restored_directory,omitempty"`
	Mappings          []RestoreMapping `json:"mappings,omitempty"`
	ReviewToken       string           `json:"review_token,omitempty"`
	ResumeServices    bool             `json:"resume_services,omitempty"`
	SnapshotID        string           `json:"snapshot_id"`
	Target            string           `json:"target,omitempty"`
	Includes          []string         `json:"includes,omitempty"`
	Test              bool             `json:"test,omitempty"`
}

type RestoreResult struct {
	DestinationID string    `json:"destination_id"`
	SnapshotID    string    `json:"snapshot_id"`
	Target        string    `json:"target"`
	Test          bool      `json:"test"`
	Manifest      *Manifest `json:"manifest,omitempty"`
}
