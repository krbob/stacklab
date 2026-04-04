package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"stacklab/internal/stacks"
)

const wsStatsInterval = 2 * time.Second

type wsSubscription struct {
	stop   chan struct{}
	cancel func()
	once   sync.Once
}

type statsSample struct {
	rxBytes   uint64
	txBytes   uint64
	timestamp time.Time
}

type dockerStatsRecord struct {
	ID       string
	Name     string
	CPU      float64
	Memory   uint64
	MemLimit uint64
	NetRX    uint64
	NetTX    uint64
}

func newWSSubscription(cancel func()) *wsSubscription {
	if cancel == nil {
		cancel = func() {}
	}
	return &wsSubscription{
		stop:   make(chan struct{}),
		cancel: cancel,
	}
}

func (s *wsSubscription) Close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stop)
		s.cancel()
	})
}

func (h *Handler) subscribeLogStream(ctx context.Context, wsConn *wsConnection, subscriptions map[string]*wsSubscription, frame wsClientFrame) error {
	var payload struct {
		StackID      string   `json:"stack_id"`
		ServiceNames []string `json:"service_names"`
		Tail         *int     `json:"tail"`
		Timestamps   bool     `json:"timestamps"`
	}
	if err := json.Unmarshal(frame.Payload, &payload); err != nil || strings.TrimSpace(payload.StackID) == "" || strings.TrimSpace(frame.StreamID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid logs.subscribe payload."))
	}

	stackDetail, err := h.stackReader.Get(ctx, payload.StackID)
	if err != nil {
		if errors.Is(err, stacks.ErrNotFound) {
			return wsConn.writeJSON(notFoundErrorFrame(frame, "Stack was not found."))
		}
		return wsConn.writeJSON(internalErrorFrame(frame, "Failed to subscribe to logs."))
	}
	if !stackDetail.Stack.Capabilities.CanViewLogs {
		return wsConn.writeJSON(validationErrorFrame(frame, "Logs are not available for this stack."))
	}

	containers := filterContainersByService(stackDetail.Stack.Containers, payload.ServiceNames)
	if existing, ok := subscriptions[frame.StreamID]; ok {
		existing.Close()
	}

	subCtx, cancel := context.WithCancel(ctx)
	subscription := newWSSubscription(cancel)
	subscriptions[frame.StreamID] = subscription

	if err := wsConn.writeJSON(wsServerFrame{
		Type:      "ack",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Payload: map[string]any{
			"status": "subscribed",
		},
	}); err != nil {
		subscription.Close()
		return err
	}

	tail := resolveLogTail(payload.Tail)
	for _, container := range containers {
		go h.forwardContainerLogs(subCtx, wsConn, frame.StreamID, container, tail)
	}

	return nil
}

func (h *Handler) subscribeStatsStream(ctx context.Context, wsConn *wsConnection, subscriptions map[string]*wsSubscription, frame wsClientFrame) error {
	var payload struct {
		StackID string `json:"stack_id"`
	}
	if err := json.Unmarshal(frame.Payload, &payload); err != nil || strings.TrimSpace(payload.StackID) == "" || strings.TrimSpace(frame.StreamID) == "" {
		return wsConn.writeJSON(validationErrorFrame(frame, "Invalid stats.subscribe payload."))
	}

	stackDetail, err := h.stackReader.Get(ctx, payload.StackID)
	if err != nil {
		if errors.Is(err, stacks.ErrNotFound) {
			return wsConn.writeJSON(notFoundErrorFrame(frame, "Stack was not found."))
		}
		return wsConn.writeJSON(internalErrorFrame(frame, "Failed to subscribe to stats."))
	}
	if !stackDetail.Stack.Capabilities.CanViewStats {
		return wsConn.writeJSON(validationErrorFrame(frame, "Stats are not available for this stack."))
	}

	if existing, ok := subscriptions[frame.StreamID]; ok {
		existing.Close()
	}

	subCtx, cancel := context.WithCancel(ctx)
	subscription := newWSSubscription(cancel)
	subscriptions[frame.StreamID] = subscription

	if err := wsConn.writeJSON(wsServerFrame{
		Type:      "ack",
		RequestID: frame.RequestID,
		StreamID:  frame.StreamID,
		Payload: map[string]any{
			"status": "subscribed",
		},
	}); err != nil {
		subscription.Close()
		return err
	}

	go h.forwardStackStats(subCtx, wsConn, frame.StreamID, payload.StackID, subscription.stop)
	return nil
}

func (h *Handler) forwardContainerLogs(ctx context.Context, wsConn *wsConnection, streamID string, container stacks.Container, tail int) {
	args := []string{"logs", "--follow", "--tail", strconv.Itoa(tail), "--timestamps", container.ID}
	cmd := exec.CommandContext(ctx, "docker", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Failed to open container log stream."))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Failed to open container log stream."))
		return
	}
	if err := cmd.Start(); err != nil {
		_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Failed to start container log stream."))
		return
	}

	var readers sync.WaitGroup
	readers.Add(2)
	go func() {
		defer readers.Done()
		scanLogStream(ctx, wsConn, streamID, container, "stdout", stdout)
	}()
	go func() {
		defer readers.Done()
		scanLogStream(ctx, wsConn, streamID, container, "stderr", stderr)
	}()
	readers.Wait()

	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Container log stream terminated unexpectedly."))
	}
}

func (h *Handler) forwardStackStats(ctx context.Context, wsConn *wsConnection, streamID string, stackID string, stop <-chan struct{}) {
	ticker := time.NewTicker(wsStatsInterval)
	defer ticker.Stop()

	previous := map[string]statsSample{}

	sendFrame := func(now time.Time) bool {
		stackDetail, err := h.stackReader.Get(ctx, stackID)
		if err != nil {
			if errors.Is(err, stacks.ErrNotFound) {
				_ = wsConn.writeJSON(streamErrorFrame(streamID, "not_found", "Stack was not found."))
			} else {
				_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Failed to collect stack stats."))
			}
			return false
		}

		containers := stackDetail.Stack.Containers
		totals := map[string]float64{
			"cpu_percent":              0,
			"memory_bytes":             0,
			"memory_limit_bytes":       0,
			"network_rx_bytes_per_sec": 0,
			"network_tx_bytes_per_sec": 0,
		}
		containerPayloads := make([]map[string]any, 0, len(containers))
		if len(containers) > 0 {
			statsByID, err := collectDockerStats(ctx, containers)
			if err != nil {
				_ = wsConn.writeJSON(streamErrorFrame(streamID, "internal_error", "Failed to collect container stats."))
				return false
			}
			sort.Slice(containers, func(i, j int) bool {
				if containers[i].ServiceName == containers[j].ServiceName {
					return containers[i].Name < containers[j].Name
				}
				return containers[i].ServiceName < containers[j].ServiceName
			})
			for _, container := range containers {
				record, ok := statsByID[container.ID]
				if !ok {
					continue
				}
				rxRate, txRate := calculateNetworkRates(previous[container.ID], record, now)
				previous[container.ID] = statsSample{
					rxBytes:   record.NetRX,
					txBytes:   record.NetTX,
					timestamp: now,
				}
				totals["cpu_percent"] += record.CPU
				totals["memory_bytes"] += float64(record.Memory)
				totals["memory_limit_bytes"] += float64(record.MemLimit)
				totals["network_rx_bytes_per_sec"] += rxRate
				totals["network_tx_bytes_per_sec"] += txRate

				containerPayloads = append(containerPayloads, map[string]any{
					"container_id":             container.ID,
					"service_name":             container.ServiceName,
					"cpu_percent":              record.CPU,
					"memory_bytes":             record.Memory,
					"memory_limit_bytes":       record.MemLimit,
					"network_rx_bytes_per_sec": rxRate,
					"network_tx_bytes_per_sec": txRate,
				})
			}
		}

		return wsConn.writeJSON(wsServerFrame{
			Type:     "stats.frame",
			StreamID: streamID,
			Payload: map[string]any{
				"timestamp": now.UTC().Format(time.RFC3339Nano),
				"stack_totals": map[string]any{
					"cpu_percent":              roundFloat(totals["cpu_percent"]),
					"memory_bytes":             uint64(totals["memory_bytes"]),
					"memory_limit_bytes":       uint64(totals["memory_limit_bytes"]),
					"network_rx_bytes_per_sec": roundFloat(totals["network_rx_bytes_per_sec"]),
					"network_tx_bytes_per_sec": roundFloat(totals["network_tx_bytes_per_sec"]),
				},
				"containers": containerPayloads,
			},
		}) == nil
	}

	if !sendFrame(time.Now().UTC()) {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case tick := <-ticker.C:
			if !sendFrame(tick.UTC()) {
				return
			}
		}
	}
}

func scanLogStream(ctx context.Context, wsConn *wsConnection, streamID string, container stacks.Container, stream string, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, 64*1024)
	scanner.Buffer(buffer, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		timestamp, line := parseTimestampedLogLine(scanner.Text())
		if err := wsConn.writeJSON(wsServerFrame{
			Type:     "logs.event",
			StreamID: streamID,
			Payload: map[string]any{
				"entries": []map[string]any{
					{
						"timestamp":    timestamp.UTC().Format(time.RFC3339Nano),
						"service_name": container.ServiceName,
						"container_id": container.ID,
						"stream":       stream,
						"line":         line,
					},
				},
			},
		}); err != nil {
			return
		}
	}
}

func collectDockerStats(ctx context.Context, containers []stacks.Container) (map[string]dockerStatsRecord, error) {
	if len(containers) == 0 {
		return map[string]dockerStatsRecord{}, nil
	}

	args := []string{"stats", "--no-stream", "--format", "{{json .}}"}
	for _, container := range containers {
		args = append(args, container.ID)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}

	records := map[string]dockerStatsRecord{}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		record, err := parseDockerStatsLine(line)
		if err != nil {
			return nil, err
		}
		records[record.ID] = record
	}
	return records, nil
}

func parseDockerStatsLine(line string) (dockerStatsRecord, error) {
	var payload struct {
		ID        string `json:"ID"`
		Container string `json:"Container"`
		Name      string `json:"Name"`
		CPUPerc   string `json:"CPUPerc"`
		MemUsage  string `json:"MemUsage"`
		NetIO     string `json:"NetIO"`
	}
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return dockerStatsRecord{}, fmt.Errorf("decode docker stats line: %w", err)
	}

	containerID := strings.TrimSpace(payload.Container)
	if containerID == "" {
		containerID = strings.TrimSpace(payload.ID)
	}
	if containerID == "" {
		return dockerStatsRecord{}, errors.New("docker stats line is missing container id")
	}

	cpuPercent, err := parsePercent(payload.CPUPerc)
	if err != nil {
		return dockerStatsRecord{}, err
	}
	memUsage, memLimit, err := parseUsagePair(payload.MemUsage)
	if err != nil {
		return dockerStatsRecord{}, err
	}
	netRX, netTX, err := parseUsagePair(payload.NetIO)
	if err != nil {
		return dockerStatsRecord{}, err
	}

	return dockerStatsRecord{
		ID:       containerID,
		Name:     payload.Name,
		CPU:      cpuPercent,
		Memory:   memUsage,
		MemLimit: memLimit,
		NetRX:    netRX,
		NetTX:    netTX,
	}, nil
}

func parsePercent(value string) (float64, error) {
	trimmed := strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if trimmed == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, fmt.Errorf("parse percent %q: %w", value, err)
	}
	return parsed, nil
}

func parseUsagePair(value string) (uint64, uint64, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("parse usage pair %q: invalid format", value)
	}

	left, err := parseHumanSize(parts[0])
	if err != nil {
		return 0, 0, err
	}
	right, err := parseHumanSize(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return left, right, nil
}

func parseHumanSize(value string) (uint64, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "i", "i"))
	if trimmed == "" || trimmed == "--" || trimmed == "0" {
		return 0, nil
	}

	trimmed = strings.TrimSuffix(trimmed, "/s")
	var split int
	for split = 0; split < len(trimmed); split++ {
		char := trimmed[split]
		if !(char == '.' || char == '-' || (char >= '0' && char <= '9')) {
			break
		}
	}

	numberPart := strings.TrimSpace(trimmed[:split])
	unitPart := strings.TrimSpace(trimmed[split:])
	if numberPart == "" {
		return 0, fmt.Errorf("parse size %q: missing number", value)
	}

	number, err := strconv.ParseFloat(numberPart, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", value, err)
	}

	multiplier := humanSizeMultiplier(unitPart)
	if multiplier == 0 {
		return 0, fmt.Errorf("parse size %q: unknown unit %q", value, unitPart)
	}

	return uint64(math.Round(number * multiplier)), nil
}

func humanSizeMultiplier(unit string) float64 {
	switch strings.TrimSpace(unit) {
	case "", "B":
		return 1
	case "kB", "KB":
		return 1000
	case "MB":
		return 1000 * 1000
	case "GB":
		return 1000 * 1000 * 1000
	case "TB":
		return 1000 * 1000 * 1000 * 1000
	case "KiB":
		return 1024
	case "MiB":
		return 1024 * 1024
	case "GiB":
		return 1024 * 1024 * 1024
	case "TiB":
		return 1024 * 1024 * 1024 * 1024
	default:
		return 0
	}
}

func calculateNetworkRates(previous statsSample, current dockerStatsRecord, now time.Time) (float64, float64) {
	if previous.timestamp.IsZero() {
		return 0, 0
	}
	elapsed := now.Sub(previous.timestamp).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}

	var rxDelta float64
	if current.NetRX >= previous.rxBytes {
		rxDelta = float64(current.NetRX - previous.rxBytes)
	}
	var txDelta float64
	if current.NetTX >= previous.txBytes {
		txDelta = float64(current.NetTX - previous.txBytes)
	}

	return roundFloat(rxDelta / elapsed), roundFloat(txDelta / elapsed)
}

func parseTimestampedLogLine(line string) (time.Time, string) {
	parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
	if len(parts) == 2 {
		if parsed, err := time.Parse(time.RFC3339Nano, parts[0]); err == nil {
			return parsed.UTC(), parts[1]
		}
	}
	return time.Now().UTC(), line
}

func filterContainersByService(containers []stacks.Container, serviceNames []string) []stacks.Container {
	if len(serviceNames) == 0 {
		return containers
	}

	allowed := make(map[string]struct{}, len(serviceNames))
	for _, name := range serviceNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowed[trimmed] = struct{}{}
	}
	if len(allowed) == 0 {
		return containers
	}

	filtered := make([]stacks.Container, 0, len(containers))
	for _, container := range containers {
		if _, ok := allowed[container.ServiceName]; ok {
			filtered = append(filtered, container)
		}
	}
	return filtered
}

func roundFloat(value float64) float64 {
	return math.Round(value*100) / 100
}

func resolveLogTail(tail *int) int {
	if tail == nil {
		return 200
	}
	return max(*tail, 0)
}
