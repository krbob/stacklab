package stacks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// StepProgress is a point-in-time aggregate of a streaming compose action,
// translated from compose's own progress events into a provider-neutral shape
// (dashboard read-model contract, Slice C).
type StepProgress struct {
	Completed int
	Total     int
	Detail    string
}

// composeProgressEvent mirrors the JSONL emitted by
// `docker compose --progress json` on stderr. Fields are parsed defensively:
// anything that does not decode is treated as plain log output.
type composeProgressEvent struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Status string `json:"status"`
}

const progressEmitInterval = 500 * time.Millisecond

// RunMaintenanceStepStreaming behaves like RunMaintenanceStep but reports
// structured progress while the compose command runs. onProgress may be nil.
func (s *ServiceReader) RunMaintenanceStepStreaming(ctx context.Context, stackID, action string, options MaintenanceStepOptions, onProgress func(StepProgress)) (string, error) {
	stack, err := s.findStack(ctx, stackID)
	if err != nil {
		return "", err
	}
	if !containsString(stack.availableActions(), "up") {
		return "", ErrInvalidState
	}

	switch action {
	case "pull":
		return s.runComposeActionStreaming(ctx, stack, onProgress, "pull")
	case "build":
		return s.runComposeActionStreaming(ctx, stack, onProgress, "build")
	case "up":
		args := []string{"-d"}
		if options.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
		return s.runComposeActionStreaming(ctx, stack, onProgress, "up", args...)
	default:
		return "", ErrUnsupportedAction
	}
}

// jsonProgressUnsupported flips permanently once the local compose rejects
// --progress json (added around compose v2.30); older versions stream
// --progress plain instead, parsed heuristically.
var jsonProgressUnsupported atomic.Bool

const unsupportedJSONProgressMarker = `unsupported --progress value "json"`

func (s *ServiceReader) runComposeActionStreaming(ctx context.Context, stack discoveredStack, onProgress func(StepProgress), action string, extraArgs ...string) (string, error) {
	if !jsonProgressUnsupported.Load() {
		combined, err := s.runComposeProgressMode(ctx, stack, "json", onProgress, action, extraArgs...)
		if err != nil && strings.Contains(err.Error(), unsupportedJSONProgressMarker) {
			jsonProgressUnsupported.Store(true)
			if s.logger != nil {
				s.logger.Info("compose does not support --progress json; falling back to plain parsing")
			}
		} else {
			return combined, err
		}
	}
	return s.runComposeProgressMode(ctx, stack, "plain", onProgress, action, extraArgs...)
}

func (s *ServiceReader) runComposeProgressMode(ctx context.Context, stack discoveredStack, mode string, onProgress func(StepProgress), action string, extraArgs ...string) (string, error) {
	command, args := composeCommand(ctx, stack.RootPath, stack.ComposeFilePath, stack.EnvFilePath)
	args = append(args, "--progress", mode, action)
	args = append(args, extraArgs...)

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = stack.RootPath

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}

	var stderrText bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if mode == "json" {
			consumeComposeProgress(stderrPipe, &stderrText, onProgress)
		} else {
			consumePlainProgress(stderrPipe, &stderrText, onProgress)
		}
	}()
	wg.Wait()

	runErr := cmd.Wait()
	combined := strings.TrimSpace(strings.TrimSpace(stdout.String()) + "\n" + strings.TrimSpace(stderrText.String()))
	if runErr != nil {
		message := combined
		if message == "" {
			message = runErr.Error()
		}
		return combined, &composeError{message: message}
	}
	return combined, nil
}

var (
	plainLayerPattern     = regexp.MustCompile(`^\s*([0-9a-f]{10,12})\s+(.+)$`)
	plainContainerPattern = regexp.MustCompile(`^\s*Container\s+(\S+)\s+(\S+)`)
)

var plainDoneStatuses = map[string]bool{
	"Pull complete":  true,
	"Already exists": true,
	"Started":        true,
	"Running":        true,
	"Healthy":        true,
	"Removed":        true,
	"Pulled":         true,
}

// consumePlainProgress derives coarse progress from --progress plain output:
// pull layers and container lifecycle lines both follow a stable
// "<id> <status>" shape. All lines are preserved as text.
func consumePlainProgress(r interface{ Read([]byte) (int, error) }, text *bytes.Buffer, onProgress func(StepProgress)) {
	doneByID := map[string]bool{}
	lastEmit := time.Time{}
	lastCompleted := -1
	lastDetail := ""

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		text.WriteString(strings.TrimSpace(line))
		text.WriteString("\n")
		if onProgress == nil {
			continue
		}

		id, status := "", ""
		if match := plainLayerPattern.FindStringSubmatch(line); match != nil {
			id, status = match[1], strings.TrimSpace(match[2])
		} else if match := plainContainerPattern.FindStringSubmatch(line); match != nil {
			id, status = "container:"+match[1], strings.TrimSpace(match[2])
		} else {
			continue
		}

		if !doneByID[id] {
			doneByID[id] = plainDoneStatuses[status]
		}
		lastDetail = strings.TrimSpace(line)

		completed := 0
		for _, done := range doneByID {
			if done {
				completed++
			}
		}
		now := time.Now()
		if completed != lastCompleted || now.Sub(lastEmit) >= progressEmitInterval {
			lastCompleted = completed
			lastEmit = now
			onProgress(StepProgress{Completed: completed, Total: len(doneByID), Detail: lastDetail})
		}
	}

	if onProgress != nil && len(doneByID) > 0 {
		completed := 0
		for _, done := range doneByID {
			if done {
				completed++
			}
		}
		onProgress(StepProgress{Completed: completed, Total: len(doneByID), Detail: lastDetail})
	}
}

type composeError struct{ message string }

func (e *composeError) Error() string { return e.message }

// consumeComposeProgress reads compose's stderr line by line: JSON progress
// events update the aggregate (throttled), everything else is kept as text so
// error output is never lost.
func consumeComposeProgress(r interface{ Read([]byte) (int, error) }, text *bytes.Buffer, onProgress func(StepProgress)) {
	statusByID := map[string]string{}
	lastDetail := ""
	lastEmit := time.Time{}
	lastCompleted := -1

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var event composeProgressEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil || event.ID == "" {
			text.WriteString(line)
			text.WriteString("\n")
			continue
		}

		statusByID[event.ID] = event.Status
		lastDetail = strings.TrimSpace(event.ID + " " + event.Text)

		if onProgress == nil {
			continue
		}
		completed := 0
		for _, status := range statusByID {
			if strings.EqualFold(status, "Done") {
				completed++
			}
		}
		now := time.Now()
		if completed != lastCompleted || now.Sub(lastEmit) >= progressEmitInterval {
			lastCompleted = completed
			lastEmit = now
			onProgress(StepProgress{Completed: completed, Total: len(statusByID), Detail: lastDetail})
		}
	}

	if onProgress != nil && len(statusByID) > 0 {
		completed := 0
		for _, status := range statusByID {
			if strings.EqualFold(status, "Done") {
				completed++
			}
		}
		onProgress(StepProgress{Completed: completed, Total: len(statusByID), Detail: lastDetail})
	}
}
