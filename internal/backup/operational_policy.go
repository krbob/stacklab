package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const policySettingKey = "backup_policy.v1"

// Policy is public operational state; heartbeat credentials never leave the server.
type Policy struct {
	Paused              bool       `json:"paused"`
	HostReviewRequired  bool       `json:"host_review_required"`
	OverdueGraceMinutes int        `json:"overdue_grace_minutes"`
	HeartbeatConfigured bool       `json:"heartbeat_configured"`
	LastHeartbeatAt     *time.Time `json:"last_heartbeat_at,omitempty"`
	HeartbeatError      string     `json:"heartbeat_error,omitempty"`
	Error               string     `json:"error,omitempty"`
}
type PolicyInput struct {
	Paused              bool    `json:"paused"`
	AcknowledgeHost     bool    `json:"acknowledge_host,omitempty"`
	OverdueGraceMinutes int     `json:"overdue_grace_minutes"`
	HeartbeatURL        *string `json:"heartbeat_url,omitempty"`
	ClearHeartbeat      bool    `json:"clear_heartbeat,omitempty"`
}
type policyState struct {
	Paused              bool            `json:"paused"`
	HostIdentity        string          `json:"host_identity"`
	HostReviewRequired  bool            `json:"host_review_required"`
	OverdueGraceMinutes int             `json:"overdue_grace_minutes"`
	HeartbeatURL        string          `json:"heartbeat_url,omitempty"`
	LastHeartbeatAt     *time.Time      `json:"last_heartbeat_at,omitempty"`
	HeartbeatError      string          `json:"heartbeat_error,omitempty"`
	Overdue             map[string]bool `json:"overdue,omitempty"`
	Error               string          `json:"-"`
}
type OperationalAlert struct {
	PlanID, PlanName, DestinationID, DestinationName string
	DueAt                                            time.Time
}

func hostIdentity() string {
	data, err := os.ReadFile("/etc/machine-id")
	if err != nil || strings.TrimSpace(string(data)) == "" {
		host, _ := os.Hostname()
		data = []byte(host)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func (s *Service) initializePolicy(ctx context.Context) {
	s.policy = policyState{OverdueGraceMinutes: 60, Overdue: map[string]bool{}}
	raw, found, err := s.jobs.Store().AppSetting(ctx, policySettingKey)
	if err != nil || found && (strings.TrimSpace(raw) == "null" || json.Unmarshal([]byte(raw), &s.policy) != nil || s.policy.OverdueGraceMinutes < 1 || s.policy.OverdueGraceMinutes > 10080) {
		s.policy.Error = "Backup scheduling policy could not be loaded; automatic backup and retention are paused."
		return
	}
	current := hostIdentity()
	if s.policy.HostIdentity != "" && s.policy.HostIdentity != current {
		s.policy.Paused, s.policy.HostReviewRequired = true, true
	}
	if s.policy.HostIdentity == "" {
		// Legacy state may be an in-place upgrade or a recovery on another host.
		// Without a recorded identity, automatic capture and retention need review.
		if len(s.state.Plans) > 0 {
			s.policy.Paused, s.policy.HostReviewRequired = true, true
		}
		s.policy.HostIdentity = current
	}
	if s.policy.Overdue == nil {
		s.policy.Overdue = map[string]bool{}
	}
	if err = s.persistPolicyLocked(ctx); err != nil {
		s.policy.Error = "Backup scheduling policy could not be saved; automatic backup and retention are paused."
	}
}
func (s *Service) persistPolicyLocked(ctx context.Context) error {
	data, err := json.Marshal(s.policy)
	if err != nil {
		return err
	}
	return s.jobs.Store().SetAppSetting(ctx, policySettingKey, string(data), s.now())
}
func (s *Service) Policy() Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policyViewLocked()
}
func (s *Service) policyViewLocked() Policy {
	return Policy{Paused: s.policy.Paused, HostReviewRequired: s.policy.HostReviewRequired, OverdueGraceMinutes: s.policy.OverdueGraceMinutes, HeartbeatConfigured: s.policy.HeartbeatURL != "", LastHeartbeatAt: s.policy.LastHeartbeatAt, HeartbeatError: s.policy.HeartbeatError, Error: s.policy.Error}
}
func (s *Service) SchedulingPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy.Paused || s.policy.HostReviewRequired || s.policy.Error != "" || s.initErr != nil || s.recoveryError != ""
}
func (s *Service) SavePolicy(ctx context.Context, input PolicyInput) (Policy, error) {
	if input.OverdueGraceMinutes < 1 || input.OverdueGraceMinutes > 10080 {
		return Policy{}, fmt.Errorf("%w: overdue_grace_minutes must be 1 to 10080", ErrInvalid)
	}
	if input.HeartbeatURL != nil && strings.TrimSpace(*input.HeartbeatURL) != "" {
		value := strings.TrimSpace(*input.HeartbeatURL)
		parsed, err := url.Parse(value)
		if err != nil || len(value) > 4096 || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" || strings.ContainsAny(value, "\r\n\x00") {
			return Policy{}, fmt.Errorf("%w: heartbeat must be an HTTP(S) URL without user information or fragment", ErrInvalid)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy.Error != "" {
		return Policy{}, fmt.Errorf("%w: repair the stored scheduling policy before changing it", ErrBusy)
	}
	if s.policy.HostReviewRequired && !input.Paused && !input.AcknowledgeHost {
		return Policy{}, fmt.Errorf("%w: review recovered sources and acknowledge this host before resuming", ErrInvalid)
	}
	old := clone(s.policy)
	s.policy.Paused = input.Paused
	s.policy.OverdueGraceMinutes = input.OverdueGraceMinutes
	if input.AcknowledgeHost {
		s.policy.HostIdentity = hostIdentity()
		s.policy.HostReviewRequired = false
	}
	if input.ClearHeartbeat {
		s.policy.HeartbeatURL = ""
		s.policy.LastHeartbeatAt = nil
		s.policy.HeartbeatError = ""
	} else if input.HeartbeatURL != nil && strings.TrimSpace(*input.HeartbeatURL) != "" {
		s.policy.HeartbeatURL = strings.TrimSpace(*input.HeartbeatURL)
		s.policy.LastHeartbeatAt = nil
		s.policy.HeartbeatError = ""
	}
	if err := s.persistPolicyLocked(ctx); err != nil {
		s.policy = old
		return Policy{}, err
	}
	return s.policyViewLocked(), nil
}
func (s *Service) SetOperationalNotifier(notify func(context.Context, OperationalAlert) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.operationalNotify = notify
}

// RunOperationalBackground is separate from transfer scheduling so a stalled
// copy cannot suppress overdue checks or prevent external monitoring.
func (s *Service) RunOperationalBackground(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.observeProtection(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) observeProtection(ctx context.Context) {
	if s.SchedulingPaused() {
		return
	}
	s.mu.Lock()
	now := s.now()
	grace := time.Duration(s.policy.OverdueGraceMinutes) * time.Minute
	alerts := []OperationalAlert{}
	overdue := map[string]bool{}
	expected, heartbeatHealthy := 0, true
	for _, plan := range s.state.Plans {
		if !plan.Enabled {
			continue
		}
		protection := s.protectionLocked(plan)
		for _, assignment := range plan.Destinations {
			if !assignment.Enabled || assignment.Schedule.Frequency == "" || assignment.Schedule.Frequency == "manual" {
				continue
			}
			expected++
			var copy CopyProtection
			for _, candidate := range protection.Copies {
				if candidate.DestinationID == assignment.DestinationID {
					copy = candidate
					break
				}
			}
			fresh := copy.LastCaptureAt != nil && !copy.LastCaptureAt.After(now.Add(clockSkewTolerance))
			if copy.DueAt != nil && (copy.LastCaptureAt == nil || copy.LastCaptureAt.Before(*copy.DueAt)) {
				// A newly configured schedule starts in the future, without backfilling
				// an occurrence preceding its durable initial marker.
				firstFuture := assignment.ScheduleActivatedAt != nil && copy.DueAt.Before(*assignment.ScheduleActivatedAt)
				if !firstFuture && now.After(copy.DueAt.Add(grace)) {
					fresh = false
					key := checkpointKey(plan.ID, assignment.DestinationID)
					overdue[key] = true
					if !s.policy.Overdue[key] {
						alerts = append(alerts, OperationalAlert{PlanID: plan.ID, PlanName: plan.Name, DestinationID: assignment.DestinationID, DestinationName: s.state.Destinations[assignment.DestinationID].Name, DueAt: *copy.DueAt})
					}
				}
			}
			if !fresh || !copy.EvidenceUsable {
				heartbeatHealthy = false
			}
		}
	}
	changed := false
	for key := range s.policy.Overdue {
		if !overdue[key] {
			delete(s.policy.Overdue, key)
			changed = true
		}
	}
	if changed {
		_ = s.persistPolicyLocked(ctx)
	}
	notify := s.operationalNotify
	target := s.policy.HeartbeatURL
	heartbeatDue := s.policy.LastHeartbeatAt == nil || now.Sub(*s.policy.LastHeartbeatAt) >= time.Minute || s.policy.LastHeartbeatAt.After(now.Add(clockSkewTolerance))
	s.mu.Unlock()
	for _, alert := range alerts {
		if notify == nil {
			break
		}
		alertCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := notify(alertCtx, alert)
		cancel()
		if err == nil {
			s.mu.Lock()
			s.policy.Overdue[checkpointKey(alert.PlanID, alert.DestinationID)] = true
			_ = s.persistPolicyLocked(ctx)
			s.mu.Unlock()
		}
	}
	if target == "" || expected == 0 || !heartbeatHealthy || !heartbeatDue || ctx.Err() != nil {
		return
	}
	err := sendProtectionHeartbeat(ctx, target)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policy.HeartbeatURL != target {
		return
	}
	s.policy.HeartbeatError = ""
	if err != nil {
		s.policy.HeartbeatError = "External protection heartbeat failed; check the monitor address and connectivity."
	} else {
		t := s.now()
		s.policy.LastHeartbeatAt = &t
	}
	_ = s.persistPolicyLocked(ctx)
}
func sendProtectionHeartbeat(ctx context.Context, target string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return errors.New("invalid heartbeat request")
	}
	req.Header.Set("User-Agent", "Stacklab-Backup-Protection/1")
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("heartbeat delivery failed")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return errors.New("heartbeat rejected")
	}
	return nil
}
