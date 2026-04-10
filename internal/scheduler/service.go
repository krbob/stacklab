package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stacklab/internal/audit"
	"stacklab/internal/jobs"
	"stacklab/internal/maintenancejobs"
	"stacklab/internal/stacks"
	"stacklab/internal/store"
)

const (
	settingsKey = "maintenance_schedules_v1"
	runtimeKey  = "maintenance_schedules_runtime_v1"

	hostLocalTimezone = "host_local"
)

type runner interface {
	ResolveTargetStacks(ctx context.Context, mode string, stackIDs []string) ([]string, error)
	RunUpdate(ctx context.Context, request maintenancejobs.UpdateRequest, requestedBy string) (store.Job, error)
	RunPrune(ctx context.Context, request maintenancejobs.PruneRequest, requestedBy string, lockStackIDs []string) (store.Job, error)
}

type stackLister interface {
	List(ctx context.Context, query stacks.ListQuery) (stacks.StackListResponse, error)
}

type Service struct {
	store        *store.Store
	audit        *audit.Service
	runner       runner
	stackLister  stackLister
	logger       *slog.Logger
	now          func() time.Time
	pollInterval time.Duration

	mu        sync.Mutex
	persistMu sync.Mutex
	running   map[string]bool
}

func NewService(appStore *store.Store, auditService *audit.Service, runner runner, stackLister stackLister, logger *slog.Logger) *Service {
	return &Service{
		store:        appStore,
		audit:        auditService,
		runner:       runner,
		stackLister:  stackLister,
		logger:       logger,
		now:          func() time.Time { return time.Now().UTC() },
		pollInterval: 30 * time.Second,
		running: map[string]bool{
			"update": false,
			"prune":  false,
		},
	}
}

func (s *Service) GetSettings(ctx context.Context) (SettingsResponse, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}
	runtimeState, err := s.loadRuntimeState(ctx)
	if err != nil {
		return SettingsResponse{}, err
	}

	now := s.now()
	return SettingsResponse{
		Timezone: hostLocalTimezone,
		Update: ScheduledUpdatePolicy{
			UpdateScheduleConfig: settings.Update,
			Status:               s.buildStatus(settings.Update.Enabled, settings.Update.Frequency, settings.Update.Time, settings.Update.Weekdays, runtimeState.Update, now),
		},
		Prune: ScheduledPrunePolicy{
			PruneScheduleConfig: settings.Prune,
			Status:              s.buildStatus(settings.Prune.Enabled, settings.Prune.Frequency, settings.Prune.Time, settings.Prune.Weekdays, runtimeState.Prune, now),
		},
	}, nil
}

func (s *Service) UpdateSettings(ctx context.Context, request UpdateSettingsRequest) (SettingsResponse, error) {
	settings := Settings{
		Update: normalizeUpdateConfig(request.Update),
		Prune:  normalizePruneConfig(request.Prune),
	}
	if err := s.validateSettings(ctx, settings); err != nil {
		return SettingsResponse{}, err
	}
	if err := s.saveSettings(ctx, settings); err != nil {
		return SettingsResponse{}, err
	}
	return s.GetSettings(ctx)
}

func (s *Service) StartBackground(ctx context.Context) {
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	s.runDueSchedules(ctx)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runDueSchedules(ctx)
		}
	}
}

func (s *Service) runDueSchedules(ctx context.Context) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		s.logWarn("load scheduler settings failed", err)
		return
	}
	runtimeState, err := s.loadRuntimeState(ctx)
	if err != nil {
		s.logWarn("load scheduler runtime failed", err)
		return
	}

	now := s.now()
	s.evaluateUpdate(ctx, settings.Update, runtimeState.Update, now)
	s.evaluatePrune(ctx, settings.Prune, runtimeState.Prune, now)
}

func (s *Service) evaluateUpdate(ctx context.Context, config UpdateScheduleConfig, runtime scheduleRuntimeState, now time.Time) {
	dueAt, ok := dueSchedule(config.Enabled, config.Frequency, config.Time, config.Weekdays, runtime.LastScheduledFor, now)
	if !ok || !s.tryStart("update") {
		return
	}

	if err := s.updateRuntime(ctx, "update", func(state *scheduleRuntimeState) {
		nowCopy := now
		dueCopy := dueAt
		state.LastTriggeredAt = &nowCopy
		state.LastScheduledFor = &dueCopy
		state.LastResult = "running"
		state.LastMessage = ""
		state.LastJobID = ""
	}); err != nil {
		s.finish("update")
		s.logWarn("persist update schedule runtime failed", err)
		return
	}

	go func() {
		defer s.finish("update")
		job, runErr := s.runner.RunUpdate(ctx, maintenancejobs.UpdateRequest{
			Target:      config.Target,
			Options:     config.Options,
			Trigger:     "scheduled",
			ScheduleKey: "update",
		}, "scheduler")
		s.finalizeScheduledRun(ctx, "update", dueAt, job, runErr)
	}()
}

func (s *Service) evaluatePrune(ctx context.Context, config PruneScheduleConfig, runtime scheduleRuntimeState, now time.Time) {
	dueAt, ok := dueSchedule(config.Enabled, config.Frequency, config.Time, config.Weekdays, runtime.LastScheduledFor, now)
	if !ok || !s.tryStart("prune") {
		return
	}

	if err := s.updateRuntime(ctx, "prune", func(state *scheduleRuntimeState) {
		nowCopy := now
		dueCopy := dueAt
		state.LastTriggeredAt = &nowCopy
		state.LastScheduledFor = &dueCopy
		state.LastResult = "running"
		state.LastMessage = ""
		state.LastJobID = ""
	}); err != nil {
		s.finish("prune")
		s.logWarn("persist prune schedule runtime failed", err)
		return
	}

	go func() {
		defer s.finish("prune")
		lockStackIDs, err := s.listManagedStackIDs(ctx)
		if err != nil {
			s.recordScheduleFailure(ctx, "prune", dueAt, err)
			return
		}
		job, runErr := s.runner.RunPrune(ctx, maintenancejobs.PruneRequest{
			Scope:       config.Scope,
			Trigger:     "scheduled",
			ScheduleKey: "prune",
		}, "scheduler", lockStackIDs)
		s.finalizeScheduledRun(ctx, "prune", dueAt, job, runErr)
	}()
}

func (s *Service) finalizeScheduledRun(ctx context.Context, scheduleKey string, scheduledFor time.Time, job store.Job, runErr error) {
	result := "succeeded"
	message := ""
	jobID := ""
	if job.ID != "" {
		jobID = job.ID
		result = job.State
		message = job.ErrorMessage
	}
	if runErr != nil {
		switch {
		case errors.Is(runErr, jobs.ErrStackLocked):
			result = "skipped"
			message = "Another maintenance job was already running."
		default:
			result = "failed"
			message = runErr.Error()
		}
		finishedAt := s.now()
		_ = s.audit.RecordSystemEvent(ctx, "run_maintenance_schedule", "scheduler", result, scheduledFor, &finishedAt, map[string]any{
			"schedule_key":  scheduleKey,
			"scheduled_for": scheduledFor.UTC().Format(time.RFC3339),
			"message":       message,
		})
	}

	if err := s.updateRuntime(ctx, scheduleKey, func(state *scheduleRuntimeState) {
		state.LastResult = result
		state.LastMessage = message
		state.LastJobID = jobID
	}); err != nil {
		s.logWarn("persist schedule runtime result failed", err)
	}
}

func (s *Service) recordScheduleFailure(ctx context.Context, scheduleKey string, scheduledFor time.Time, err error) {
	message := err.Error()
	if persistErr := s.updateRuntime(ctx, scheduleKey, func(state *scheduleRuntimeState) {
		state.LastResult = "failed"
		state.LastMessage = message
		state.LastJobID = ""
	}); persistErr != nil {
		s.logWarn("persist schedule failure failed", persistErr)
	}
	finishedAt := s.now()
	_ = s.audit.RecordSystemEvent(ctx, "run_maintenance_schedule", "scheduler", "failed", scheduledFor, &finishedAt, map[string]any{
		"schedule_key":  scheduleKey,
		"scheduled_for": scheduledFor.UTC().Format(time.RFC3339),
		"message":       message,
	})
}

func (s *Service) buildStatus(enabled bool, frequency Frequency, timeOfDay string, weekdays []Weekday, runtime scheduleRuntimeState, now time.Time) ScheduleStatus {
	var nextRunAt *time.Time
	if enabled {
		if next, err := nextRun(frequency, timeOfDay, weekdays, now); err == nil {
			nextRunAt = &next
		}
	}
	status := ScheduleStatus{
		NextRunAt:        nextRunAt,
		LastTriggeredAt:  runtime.LastTriggeredAt,
		LastScheduledFor: runtime.LastScheduledFor,
		LastResult:       runtime.LastResult,
		LastMessage:      runtime.LastMessage,
	}
	if runtime.LastJobID != "" {
		jobID := runtime.LastJobID
		status.LastJobID = &jobID
	}
	return status
}

func (s *Service) validateSettings(ctx context.Context, settings Settings) error {
	if err := validateUpdateConfig(ctx, s.runner, settings.Update); err != nil {
		return err
	}
	if err := validatePruneConfig(settings.Prune); err != nil {
		return err
	}
	return nil
}

func defaultSettings() Settings {
	return Settings{
		Update: UpdateScheduleConfig{
			Enabled:   false,
			Frequency: FrequencyWeekly,
			Time:      "03:30",
			Weekdays:  []Weekday{WeekdaySaturday},
			Target: maintenancejobs.UpdateTarget{
				Mode: "all",
			},
			Options: maintenancejobs.UpdateOptions{
				PullImages:     true,
				BuildImages:    true,
				RemoveOrphans:  true,
				PruneAfter:     false,
				IncludeVolumes: false,
			},
		},
		Prune: PruneScheduleConfig{
			Enabled:   false,
			Frequency: FrequencyWeekly,
			Time:      "04:30",
			Weekdays:  []Weekday{WeekdaySunday},
			Scope: struct {
				Images            bool `json:"images"`
				BuildCache        bool `json:"build_cache"`
				StoppedContainers bool `json:"stopped_containers"`
				Volumes           bool `json:"volumes"`
			}{
				Images:            true,
				BuildCache:        true,
				StoppedContainers: true,
				Volumes:           false,
			},
		},
	}
}

func normalizeUpdateConfig(config UpdateScheduleConfig) UpdateScheduleConfig {
	normalized := config
	normalized.Target.Mode = strings.TrimSpace(normalized.Target.Mode)
	normalized.Target.StackIDs = dedupeStackIDs(normalized.Target.StackIDs)
	return normalized
}

func normalizePruneConfig(config PruneScheduleConfig) PruneScheduleConfig {
	return config
}

func validateUpdateConfig(ctx context.Context, runner runner, config UpdateScheduleConfig) error {
	if err := validateBaseSchedule(config.Enabled, config.Frequency, config.Time, config.Weekdays); err != nil {
		return err
	}
	if config.Target.Mode == "" {
		config.Target.Mode = "all"
	}
	switch config.Target.Mode {
	case "all":
	case "selected":
		if len(config.Target.StackIDs) == 0 {
			return errors.New("update.target.stack_ids must be non-empty when mode = selected")
		}
		if _, err := runner.ResolveTargetStacks(ctx, config.Target.Mode, config.Target.StackIDs); err != nil {
			return err
		}
	default:
		return errors.New("update.target.mode must be one of: selected, all")
	}
	if config.Options.IncludeVolumes && !config.Options.PruneAfter {
		return errors.New("update.options.include_volumes requires prune_after = true")
	}
	return nil
}

func validatePruneConfig(config PruneScheduleConfig) error {
	if err := validateBaseSchedule(config.Enabled, config.Frequency, config.Time, config.Weekdays); err != nil {
		return err
	}
	if !config.Scope.Images && !config.Scope.BuildCache && !config.Scope.StoppedContainers && !config.Scope.Volumes {
		return errors.New("prune.scope must enable at least one category")
	}
	return nil
}

func validateBaseSchedule(enabled bool, frequency Frequency, timeOfDay string, weekdays []Weekday) error {
	if _, _, err := parseTimeOfDay(timeOfDay); err != nil {
		return err
	}
	switch frequency {
	case FrequencyDaily:
		return nil
	case FrequencyWeekly:
		if len(weekdays) == 0 {
			return errors.New("weekly schedules require at least one weekday")
		}
		for _, weekday := range weekdays {
			if _, ok := weekdayToTime(weekday); !ok {
				return fmt.Errorf("unsupported weekday %q", weekday)
			}
		}
		return nil
	default:
		return errors.New("frequency must be one of: daily, weekly")
	}
}

func dueSchedule(enabled bool, frequency Frequency, timeOfDay string, weekdays []Weekday, lastScheduledFor *time.Time, now time.Time) (time.Time, bool) {
	if !enabled {
		return time.Time{}, false
	}
	dueAt, err := mostRecentScheduledAt(frequency, timeOfDay, weekdays, now.In(time.Local))
	if err != nil {
		return time.Time{}, false
	}
	if lastScheduledFor != nil && dueAt.UTC().Equal(lastScheduledFor.UTC()) {
		return time.Time{}, false
	}
	return dueAt.UTC(), true
}

func nextRun(frequency Frequency, timeOfDay string, weekdays []Weekday, now time.Time) (time.Time, error) {
	return nextScheduledAt(frequency, timeOfDay, weekdays, now.In(time.Local))
}

func mostRecentScheduledAt(frequency Frequency, timeOfDay string, weekdays []Weekday, now time.Time) (time.Time, error) {
	hour, minute, err := parseTimeOfDay(timeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	switch frequency {
	case FrequencyDaily:
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if candidate.After(now) {
			candidate = candidate.AddDate(0, 0, -1)
		}
		return candidate, nil
	case FrequencyWeekly:
		allowed := weekdaySet(weekdays)
		for offset := 0; offset <= 7; offset++ {
			candidateDay := now.AddDate(0, 0, -offset)
			if !allowed[candidateDay.Weekday()] {
				continue
			}
			candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(), hour, minute, 0, 0, now.Location())
			if candidate.After(now) {
				continue
			}
			return candidate, nil
		}
	}
	return time.Time{}, errors.New("unable to compute schedule")
}

func nextScheduledAt(frequency Frequency, timeOfDay string, weekdays []Weekday, now time.Time) (time.Time, error) {
	hour, minute, err := parseTimeOfDay(timeOfDay)
	if err != nil {
		return time.Time{}, err
	}
	switch frequency {
	case FrequencyDaily:
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		return candidate.UTC(), nil
	case FrequencyWeekly:
		allowed := weekdaySet(weekdays)
		for offset := 0; offset <= 7; offset++ {
			candidateDay := now.AddDate(0, 0, offset)
			if !allowed[candidateDay.Weekday()] {
				continue
			}
			candidate := time.Date(candidateDay.Year(), candidateDay.Month(), candidateDay.Day(), hour, minute, 0, 0, now.Location())
			if candidate.After(now) {
				return candidate.UTC(), nil
			}
		}
	}
	return time.Time{}, errors.New("unable to compute next schedule")
}

func parseTimeOfDay(value string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, errors.New("time must be in HH:MM format")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, errors.New("time must be in HH:MM format")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, errors.New("time must be in HH:MM format")
	}
	return hour, minute, nil
}

func weekdaySet(days []Weekday) map[time.Weekday]bool {
	set := make(map[time.Weekday]bool, len(days))
	for _, day := range days {
		if weekday, ok := weekdayToTime(day); ok {
			set[weekday] = true
		}
	}
	return set
}

func weekdayToTime(day Weekday) (time.Weekday, bool) {
	switch day {
	case WeekdayMonday:
		return time.Monday, true
	case WeekdayTuesday:
		return time.Tuesday, true
	case WeekdayWednesday:
		return time.Wednesday, true
	case WeekdayThursday:
		return time.Thursday, true
	case WeekdayFriday:
		return time.Friday, true
	case WeekdaySaturday:
		return time.Saturday, true
	case WeekdaySunday:
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func dedupeStackIDs(stackIDs []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		stackID = strings.TrimSpace(stackID)
		if stackID == "" {
			continue
		}
		if _, ok := seen[stackID]; ok {
			continue
		}
		seen[stackID] = struct{}{}
		result = append(result, stackID)
	}
	sort.Strings(result)
	return result
}

func (s *Service) listManagedStackIDs(ctx context.Context) ([]string, error) {
	list, err := s.stackLister.List(ctx, stacks.ListQuery{})
	if err != nil {
		return nil, err
	}
	stackIDs := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		stackIDs = append(stackIDs, item.ID)
	}
	sort.Strings(stackIDs)
	return stackIDs, nil
}

func (s *Service) loadSettings(ctx context.Context) (Settings, error) {
	raw, ok, err := s.store.AppSetting(ctx, settingsKey)
	if err != nil {
		return Settings{}, err
	}
	if !ok {
		return defaultSettings(), nil
	}
	settings := defaultSettings()
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return Settings{}, fmt.Errorf("parse maintenance schedules: %w", err)
	}
	settings.Update = normalizeUpdateConfig(settings.Update)
	settings.Prune = normalizePruneConfig(settings.Prune)
	return settings, nil
}

func (s *Service) saveSettings(ctx context.Context, settings Settings) error {
	payload, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal maintenance schedules: %w", err)
	}
	return s.store.SetAppSetting(ctx, settingsKey, string(payload), s.now())
}

func (s *Service) loadRuntimeState(ctx context.Context) (runtimeState, error) {
	raw, ok, err := s.store.AppSetting(ctx, runtimeKey)
	if err != nil {
		return runtimeState{}, err
	}
	if !ok {
		return runtimeState{}, nil
	}
	var state runtimeState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return runtimeState{}, fmt.Errorf("parse maintenance schedule runtime: %w", err)
	}
	return state, nil
}

func (s *Service) updateRuntime(ctx context.Context, scheduleKey string, mutate func(*scheduleRuntimeState)) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	state, err := s.loadRuntimeState(ctx)
	if err != nil {
		return err
	}

	switch scheduleKey {
	case "update":
		mutate(&state.Update)
	case "prune":
		mutate(&state.Prune)
	default:
		return fmt.Errorf("unknown schedule key %q", scheduleKey)
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal maintenance schedule runtime: %w", err)
	}
	return s.store.SetAppSetting(ctx, runtimeKey, string(payload), s.now())
}

func (s *Service) tryStart(scheduleKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running[scheduleKey] {
		return false
	}
	s.running[scheduleKey] = true
	return true
}

func (s *Service) finish(scheduleKey string) {
	s.mu.Lock()
	s.running[scheduleKey] = false
	s.mu.Unlock()
}

func (s *Service) logWarn(message string, err error) {
	if s.logger != nil {
		s.logger.Warn(message, slog.String("err", err.Error()))
	}
}
