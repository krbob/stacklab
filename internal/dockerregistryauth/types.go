package dockerregistryauth

import (
	"time"

	"stacklab/internal/fsmeta"
)

type StatusResponse struct {
	DockerConfigPath string              `json:"docker_config_path"`
	Exists           bool                `json:"exists"`
	Permissions      *fsmeta.Permissions `json:"permissions,omitempty"`
	SizeBytes        *int64              `json:"size_bytes,omitempty"`
	ModifiedAt       *time.Time          `json:"modified_at,omitempty"`
	ValidJSON        bool                `json:"valid_json"`
	ParseError       *string             `json:"parse_error,omitempty"`
	Items            []RegistryEntry     `json:"items"`
}

type RegistryEntry struct {
	Registry       string     `json:"registry"`
	Configured     bool       `json:"configured"`
	Username       string     `json:"username"`
	Source         string     `json:"source"`
	LastVerifiedAt *time.Time `json:"last_verified_at,omitempty"`
	LastError      string     `json:"last_error"`
}

type LoginRequest struct {
	Registry string `json:"registry"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type LogoutRequest struct {
	Registry string `json:"registry"`
}
