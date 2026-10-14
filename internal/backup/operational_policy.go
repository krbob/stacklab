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
	"sort"
	"strings"
	"time"
)

const policySettingKey = "backup_policy.v1"
const operationalAlertSettingKey = "backup_protection_alerts.v1"

// PauseReason explains a current execution constraint independently of user intent.
type PauseReason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Policy is public operational state; heartbeat credentials never leave the server.
type Policy struct {
	EffectivePaused     bool          `json:"effective_paused"`
	PauseReasons        []PauseReason `json:"pause_reasons"`
	Paused              bool          `json:"paused"`
	HostReviewRequired  bool          `json:"host_review_required"`
	OverdueGraceMinutes int           `json:"overdue_grace_minutes"`
	HeartbeatConfigured bool          `json:"heartbeat_configured"`
	LastHeartbeatAt     *time.Time    `json:"last_heartbeat_at,omitempty"`
	HeartbeatError      string        `json:"heartbeat_error,omitempty"`
	Error               string        `json:"error,omitempty"`
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
type operationalAlertState struct {
	Loaded   bool                        `json:"-"`
	Cursor   string                      `json:"cursor,omitempty"`
	Notified map[string]OperationalAlert `json:"notified"`
}

type OperationalAlert struct {
	Event                                            string
	PauseReasons                                     []PauseReason
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
		s.policy.HostReviewRequired = true
	}
	if s.policy.HostIdentity == "" {
		// Legacy state may be an in-place upgrade or a recovery on another host.
		// Without a recorded identity, automatic capture and retention need review.
		if len(s.state.Plans) > 0 {
			s.policy.HostReviewRequired = true
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
func (s *Service) pauseReasonsLocked() []PauseReason {
	reasons := []PauseReason{}
	if s.policy.Paused {
		reasons = append(reasons, PauseReason{"user_paused", "Automatic backup and retention are paused by an operator."})
	}
	if s.policy.HostReviewRequired {
		reasons = append(reasons, PauseReason{"host_review_required", "Review backup sources and destinations on this host, then acknowledge the host in Backup settings."})
	}
	if s.policy.Error != "" {
		reasons = append(reasons, PauseReason{"policy_error", "Repair the stored backup scheduling policy before resuming automatic backup and retention."})
	}
	if s.initErr != nil {
		reasons = append(reasons, PauseReason{"initialization_error", "The backup module could not initialize. Inspect the saved backup state and service logs."})
	}
	if s.recoveryError != "" {
		reasons = append(reasons, PauseReason{"service_recovery_required", "Resolve Service recovery before capturing or applying data. All plans are blocked because shared source ownership may be uncertain."})
	}
	return reasons
}
func (s *Service) policyViewLocked() Policy {
	reasons := s.pauseReasonsLocked()
	grace := s.policy.OverdueGraceMinutes
	if grace < 1 || grace > 10080 {
		grace = 60
	}
	return Policy{Paused: s.policy.Paused, EffectivePaused: len(reasons) > 0, PauseReasons: reasons, HostReviewRequired: s.policy.HostReviewRequired, OverdueGraceMinutes: grace, HeartbeatConfigured: s.policy.HeartbeatURL != "", LastHeartbeatAt: s.policy.LastHeartbeatAt, HeartbeatError: s.policy.HeartbeatError, Error: s.policy.Error}
}
func (s *Service) SchedulingPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.pauseReasonsLocked()) > 0
}

// Explicit operator pause governs automatic work; a manual capture remains
// available. Automatic safety pauses also reject manual capture with a conflict.
func (s *Service) schedulingPauseErrorLocked() error {
	for _, reason := range s.pauseReasonsLocked() {
		if reason.Code != "user_paused" {
			return fmt.Errorf("%w: %s", ErrBusy, reason.Message)
		}
	}
	return nil
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
		s.RefreshHelperReadiness(ctx)
		s.observeProtection(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Alert delivery state is separate from execution policy: observing a corrupt
// policy must never overwrite the evidence that needs operator repair.
func (s *Service) initializeOperationalAlertsLocked(ctx context.Context) {
	if s.operationalAlerts.Loaded {
		return
	}
	s.operationalAlerts = operationalAlertState{Loaded: true, Notified: map[string]OperationalAlert{}}
	raw, found, err := s.jobs.Store().AppSetting(ctx, operationalAlertSettingKey)
	if err == nil && found {
		var saved operationalAlertState
		if json.Unmarshal([]byte(raw), &saved) == nil && saved.Notified != nil {
			s.operationalAlerts.Notified = saved.Notified
			s.operationalAlerts.Cursor = saved.Cursor
		}
	}
}
func (s *Service) persistOperationalAlertsLocked(ctx context.Context) {
	data, err := json.Marshal(s.operationalAlerts)
	if err == nil {
		_ = s.jobs.Store().SetAppSetting(ctx, operationalAlertSettingKey, string(data), s.now())
	}
}
func alertSignature(alert OperationalAlert) string {
	codes := []string{alert.Event}
	for _, reason := range alert.PauseReasons {
		codes = append(codes, reason.Code)
	}
	return strings.Join(codes, "/")
}
func (s *Service) observeProtection(ctx context.Context) {
	s.mu.Lock()
	s.initializeOperationalAlertsLocked(ctx)
	now := s.now()
	graceMinutes := s.policy.OverdueGraceMinutes
	if graceMinutes < 1 || graceMinutes > 10080 {
		graceMinutes = 60
	}
	grace := time.Duration(graceMinutes) * time.Minute
	active := map[string]OperationalAlert{}
	monitored := map[string]bool{}
	resolved := map[string]bool{}
	reasons := s.pauseReasonsLocked()
	if len(reasons) > 0 {
		active["pause"] = OperationalAlert{Event: "backup_protection_paused", PauseReasons: reasons}
	}
	expected, heartbeatHealthy := 0, len(reasons) == 0
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
			key := checkpointKey(plan.ID, assignment.DestinationID)
			monitored[key] = true
			var copy CopyProtection
			for _, candidate := range protection.Copies {
				if candidate.DestinationID == assignment.DestinationID {
					copy = candidate
					break
				}
			}
			fresh := copy.LastCaptureAt != nil && !copy.LastCaptureAt.After(now.Add(clockSkewTolerance))
			pastGrace := false
			if copy.DueAt != nil && (copy.LastCaptureAt == nil || copy.LastCaptureAt.Before(*copy.DueAt)) {
				firstFuture := assignment.ScheduleActivatedAt != nil && copy.DueAt.Before(*assignment.ScheduleActivatedAt)
				if !firstFuture && now.After(copy.DueAt.Add(grace)) {
					fresh = false
					pastGrace = true
				}
			}
			if !fresh || !copy.EvidenceUsable {
				heartbeatHealthy = false
			}
			resolved[key] = fresh && copy.EvidenceUsable
			if pastGrace {
				active[key] = OperationalAlert{Event: "backup_copy_overdue", PlanID: plan.ID, PlanName: plan.Name, DestinationID: assignment.DestinationID, DestinationName: s.state.Destinations[assignment.DestinationID].Name, DueAt: *copy.DueAt}
			} else if previous, exists := s.operationalAlerts.Notified[key]; exists && !resolved[key] {
				// A fresh upload with missing/corrupt evidence does not resolve an alarm.
				active[key] = previous
			}
		}
	}
	type transition struct {
		key   string
		alert OperationalAlert
		clear bool
	}
	changes := []transition{}
	for key, alert := range active {
		previous, exists := s.operationalAlerts.Notified[key]
		if !exists || alertSignature(previous) != alertSignature(alert) {
			changes = append(changes, transition{key: key, alert: alert})
		}
	}
	for key, previous := range s.operationalAlerts.Notified {
		if _, exists := active[key]; exists {
			continue
		}
		if key != "pause" && !monitored[key] {
			delete(s.operationalAlerts.Notified, key)
			s.persistOperationalAlertsLocked(ctx)
			continue
		}
		previous.Event = "backup_copy_resolved"
		if key == "pause" {
			previous.Event = "backup_protection_resumed"
		}
		changes = append(changes, transition{key: key, alert: previous, clear: true})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].key < changes[j].key })
	// Rotate copy notifications after the last attempted key so a failing channel
	// cannot indefinitely starve later plans. System pause transitions go first.
	ordered := make([]transition, 0, len(changes))
	for _, change := range changes {
		if change.key == "pause" {
			ordered = append(ordered, change)
		}
	}
	for _, change := range changes {
		if change.key != "pause" && change.key > s.operationalAlerts.Cursor {
			ordered = append(ordered, change)
		}
	}
	for _, change := range changes {
		if change.key != "pause" && change.key <= s.operationalAlerts.Cursor {
			ordered = append(ordered, change)
		}
	}
	notify := s.operationalNotify
	target := s.policy.HeartbeatURL
	heartbeatDue := s.policy.LastHeartbeatAt == nil || now.Sub(*s.policy.LastHeartbeatAt) >= time.Minute || s.policy.LastHeartbeatAt.After(now.Add(clockSkewTolerance))
	s.mu.Unlock()
	// Healthy evidence can restore an external heartbeat even when a notification
	// channel is slow. Each HTTP heartbeat is independently bounded to ten seconds.
	if target != "" && expected > 0 && heartbeatHealthy && heartbeatDue && ctx.Err() == nil {
		s.deliverProtectionHeartbeat(ctx, target)
	}
	deliveryCtx, deliveryCancel := context.WithTimeout(ctx, 20*time.Second)
	defer deliveryCancel()
	for index, change := range ordered {
		if notify == nil || deliveryCtx.Err() != nil || index >= 16 {
			break
		}
		alertCtx, cancel := context.WithTimeout(deliveryCtx, 10*time.Second)
		err := notify(alertCtx, change.alert)
		cancel()
		s.mu.Lock()
		if change.key != "pause" {
			s.operationalAlerts.Cursor = change.key
		}
		if err == nil {
			if change.clear {
				delete(s.operationalAlerts.Notified, change.key)
			} else {
				s.operationalAlerts.Notified[change.key] = change.alert
			}
			s.policy.Overdue = map[string]bool{}
			for key := range s.operationalAlerts.Notified {
				if key != "pause" {
					s.policy.Overdue[key] = true
				}
			}
		}
		s.persistOperationalAlertsLocked(ctx)
		s.mu.Unlock()
	}
}
func (s *Service) deliverProtectionHeartbeat(ctx context.Context, target string) {
	// Recheck the live execution policy immediately before sending.
	s.mu.Lock()
	paused := len(s.pauseReasonsLocked()) > 0 || s.policy.HeartbeatURL != target
	s.mu.Unlock()
	if paused {
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
