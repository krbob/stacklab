package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"stacklab/internal/config"
	"stacklab/internal/limitedio"
	"stacklab/internal/stacks"

	"gopkg.in/yaml.v3"
)

// Source identities describe operator-selected logical content. Path pins the
// resolved source so replacing a mount never silently changes uploaded data.
type Source struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	StackID  string   `json:"stack_id,omitempty"`
	Service  string   `json:"service,omitempty"`
	Mount    string   `json:"mount,omitempty"`
	Path     string   `json:"path,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

type Writer struct {
	ContainerID        string `json:"container_id"`
	StackID            string `json:"stack_id,omitempty"`
	Service            string `json:"service,omitempty"`
	Running            bool   `json:"running"`
	ReadOnly           bool   `json:"read_only,omitempty"`
	StopTimeoutSeconds *int   `json:"stop_timeout_seconds,omitempty"`
}

type Manifest struct {
	Version                int              `json:"version"`
	RunID                  string           `json:"run_id"`
	PlanID                 string           `json:"plan_id"`
	CapturedAt             time.Time        `json:"captured_at"`
	Consistency            string           `json:"consistency"`
	Sources                []ManifestSource `json:"sources"`
	Warnings               []string         `json:"warnings,omitempty"`
	StacklabVersion        string           `json:"stacklab_version"`
	Images                 []string         `json:"images,omitempty"`
	ServiceDowntimeSeconds int64            `json:"service_downtime_seconds,omitempty"`
}

type ManifestSource struct {
	Source                 Source   `json:"source"`
	SavedPath              string   `json:"saved_path"`
	OriginalPath           string   `json:"original_path"`
	Writers                []Writer `json:"writers,omitempty"`
	Consumers              []Writer `json:"consumers,omitempty"`
	Aliases                []Source `json:"aliases,omitempty"`
	Archive                bool     `json:"archive"`
	SizeBytes              int64    `json:"size_bytes"`
	VolumeName             string   `json:"volume_name,omitempty"`
	FileName               string   `json:"file_name,omitempty"`
	IndexPath              string   `json:"index_path,omitempty"`
	IndexVersion           int      `json:"index_version,omitempty"`
	EntryCount             int      `json:"entry_count,omitempty"`
	IndexUnavailableReason string   `json:"index_unavailable_reason,omitempty"`
	RecoveryRestrictions   []string `json:"recovery_restrictions,omitempty"`
}

type captureStackReader interface {
	Get(context.Context, string) (stacks.StackDetailResponse, error)
	ResolvedConfigCurrent(context.Context, string, string) (stacks.ResolvedConfigResponse, error)
}

type NativeCapture struct {
	cfg               config.Config
	stacks            captureStackReader
	sqliteBackup      func(context.Context, string) error
	command           func(context.Context, string, ...string) ([]byte, error)
	serviceConfigPath string
}

func NewNativeCapture(cfg config.Config, reader *stacks.ServiceReader, sqliteBackup func(context.Context, string) error) *NativeCapture {
	return &NativeCapture{cfg: cfg, stacks: reader, sqliteBackup: sqliteBackup, command: captureCommand}
}

func captureCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCaptureProcess(cmd)
	output := limitedio.NewBuffer(16 * 1024 * 1024)
	cmd.Stdout = output
	cmd.Stderr = output
	err := cmd.Run()
	if output.Err() != nil {
		return nil, errors.New("backup discovery output exceeded limit")
	}
	return output.Bytes(), err
}

type captureContainer struct {
	ID    string `json:"Id"`
	State struct {
		Running bool
		Paused  bool
	}
	Config struct {
		Image       string
		StopTimeout *int
		Labels      map[string]string
	}
	Mounts []struct {
		Type, Source, Destination, Name string
		RW                              bool
	}
}

type captureVolume struct {
	Name, Driver, Mountpoint string
	Options                  map[string]string
}
type backupCompose struct {
	Services map[string]struct {
		Image   string `yaml:"image"`
		Volumes []struct {
			Type, Source, Target string
			ReadOnly             bool `yaml:"read_only"`
		} `yaml:"volumes"`
	} `yaml:"services"`
	Volumes map[string]struct {
		Name string `yaml:"name"`
	} `yaml:"volumes"`
}

func sourceID(kind, stack, service, mount string) string {
	hash := sha256.Sum256([]byte(strings.Join([]string{kind, stack, service, mount}, "\x00")))
	return "source_" + hex.EncodeToString(hash[:12])
}

func (c *NativeCapture) compose(ctx context.Context, stackID string) (backupCompose, error) {
	if !stacks.IsValidStackID(stackID) {
		return backupCompose{}, errors.New("invalid backup stack ID")
	}
	resolved, err := c.stacks.ResolvedConfigCurrent(ctx, stackID, "current")
	if err != nil {
		return backupCompose{}, err
	}
	if !resolved.Valid {
		return backupCompose{}, fmt.Errorf("cannot resolve Compose for %s", stackID)
	}
	var compose backupCompose
	if err := yaml.Unmarshal([]byte(resolved.Content), &compose); err != nil {
		return compose, fmt.Errorf("parse resolved Compose: %w", err)
	}
	return compose, nil
}

// Discover returns explicit candidates; adding a service or mount does not
// mutate an existing plan. The caller compares candidate IDs with selections.
func (c *NativeCapture) Discover(ctx context.Context, stackID string) ([]Source, error) {
	if stackID == "" {
		configPath, _ := filepath.Abs(filepath.Join(c.cfg.RootDir, "config"))
		return []Source{{ID: sourceID("stacklab", "", "", ""), Kind: "stacklab"}, {ID: sourceID("managed_config", "", "", configPath), Kind: "managed_config", Path: configPath}}, nil
	}
	detail, err := c.stacks.Get(ctx, stackID)
	if err != nil {
		return nil, err
	}
	compose, err := c.compose(ctx, stackID)
	if err != nil {
		return nil, err
	}
	result := []Source{{ID: sourceID("stack_definition", stackID, "", ""), Kind: "stack_definition", StackID: stackID, Path: detail.Stack.RootPath}}
	if info, err := os.Stat(detail.Stack.ConfigPath); err == nil && info.IsDir() {
		result = append(result, Source{ID: sourceID("managed_config", stackID, "", detail.Stack.ConfigPath), Kind: "managed_config", StackID: stackID, Path: detail.Stack.ConfigPath})
	}
	for name, service := range compose.Services {
		for _, mount := range service.Volumes {
			if mount.Type != "bind" && mount.Type != "volume" {
				continue
			}
			path := mount.Source
			if mount.Type == "volume" {
				if volume, ok := compose.Volumes[mount.Source]; ok && volume.Name != "" {
					path = volume.Name
				} else {
					path = stackID + "_" + mount.Source
				}
			}
			result = append(result, Source{ID: sourceID("service_mount", stackID, name, mount.Target), Kind: "service_mount", StackID: stackID, Service: name, Mount: mount.Target, Path: path})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func (c *NativeCapture) Inventory(ctx context.Context, stackID string) ([]Source, error) {
	return c.Discover(ctx, stackID)
}

func (c *NativeCapture) containers(ctx context.Context) ([]captureContainer, error) {
	output, err := c.command(ctx, "docker", "ps", "-aq", "--no-trunc")
	if err != nil {
		return nil, errors.New("Docker is unavailable for backup source discovery")
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return []captureContainer{}, nil
	}
	args := append([]string{"inspect", "--type", "container"}, ids...)
	output, err = c.command(ctx, "docker", args...)
	if err != nil {
		return nil, errors.New("cannot inspect backup source containers")
	}
	var containers []captureContainer
	if err = json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("decode backup containers: %w", err)
	}
	return containers, nil
}

func pathContains(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// normalizedDataPath resolves existing ancestors too, so a new restore target
// below a symlink cannot bypass private-state guards or writer coordination.
func normalizedDataPath(value string) string {
	if value == "" {
		return ""
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return filepath.Clean(value)
	}
	current := abs
	tail := []string{}
	for {
		resolved, resolveErr := filepath.EvalSymlinks(current)
		if resolveErr == nil {
			for index := len(tail) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, tail[index])
			}
			return resolved
		}
		if !errors.Is(resolveErr, os.ErrNotExist) || filepath.Dir(current) == current {
			return abs
		}
		tail = append(tail, filepath.Base(current))
		current = filepath.Dir(current)
	}
}

func pathsOverlapData(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	left = normalizedDataPath(left)
	right = normalizedDataPath(right)
	return pathContains(left, right) || pathContains(right, left)
}

func canonicalSource(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("backup source must be an absolute path")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	if resolved == string(filepath.Separator) {
		return "", errors.New("whole-host backup is not supported")
	}
	return resolved, nil
}

func (c *NativeCapture) Preview(ctx context.Context, plan Plan) ([]ManifestSource, error) {
	var containers []captureContainer
	for _, source := range plan.Sources {
		if source.Kind == "service_mount" || plan.Consistency == "stop" && source.Kind != "stacklab" {
			var err error
			containers, err = c.containers(ctx)
			if err != nil {
				return nil, err
			}
			break
		}
	}
	result := []ManifestSource{}
	physical := map[string]int{}
	for _, source := range plan.Sources {
		if source.ID == "" {
			source.ID = sourceID(source.Kind, source.StackID, source.Service, source.Mount+source.Path)
		}
		item, err := c.resolveSource(ctx, source, containers)
		if err != nil {
			return nil, fmt.Errorf("source %s: %w", source.ID, err)
		}
		key := item.OriginalPath
		if source.Kind == "stacklab" {
			key = "stacklab"
		}
		if index, ok := physical[key]; ok {
			if strings.Join(result[index].Source.Excludes, "\x00") != strings.Join(source.Excludes, "\x00") {
				return nil, errors.New("shared sources must use identical exclusions")
			}
			result[index].Aliases = append(result[index].Aliases, source)
			continue
		}
		physical[key] = len(result)
		result = append(result, item)
	}
	return result, nil
}

func (c *NativeCapture) resolveSource(ctx context.Context, source Source, containers []captureContainer) (ManifestSource, error) {
	item := ManifestSource{Source: source, Archive: true, SavedPath: filepath.ToSlash(filepath.Join("sources", sourceID(source.ID, "", "", "")+".tar"))}
	path := source.Path
	switch source.Kind {
	case "stacklab":
		item.OriginalPath = c.cfg.DataDir
		return item, nil
	case "stack_definition":
		if !stacks.IsValidStackID(source.StackID) {
			return item, errors.New("invalid stack ID")
		}
		path = filepath.Join(c.cfg.RootDir, "stacks", source.StackID)
		if info, statErr := os.Lstat(path); statErr != nil {
			return item, statErr
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return item, errors.New("stack definition root must be a real directory")
		}
	case "managed_config":
		root, err := filepath.Abs(filepath.Join(c.cfg.RootDir, "config"))
		if err != nil {
			return item, err
		}
		if path == "" {
			path = filepath.Join(root, source.StackID)
		}
		resolved, err := canonicalSource(path)
		if err != nil {
			return item, err
		}
		canonicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return item, err
		}
		if !pathContains(canonicalRoot, resolved) {
			return item, errors.New("configuration source escapes managed configuration")
		}
		path = resolved
	case "service_mount":
		compose, err := c.compose(ctx, source.StackID)
		if err != nil {
			return item, err
		}
		service, ok := compose.Services[source.Service]
		if !ok {
			return item, errors.New("selected Compose service no longer exists")
		}
		found := false
		mountKind := ""
		for _, mount := range service.Volumes {
			if mount.Target == source.Mount {
				if mount.Type != "bind" && mount.Type != "volume" {
					return item, errors.New("unsupported mount type")
				}
				found = true
				mountKind = mount.Type
				path = mount.Source
				if mount.Type == "volume" {
					if path == "" {
						return item, errors.New("anonymous volumes need an explicit Compose volume name")
					}
					if volume, ok := compose.Volumes[path]; ok && volume.Name != "" {
						path = volume.Name
					} else {
						path = source.StackID + "_" + path
					}
					if source.Path != "" && source.Path != path {
						return item, errors.New("selected volume identity changed; review the plan")
					}
					output, err := c.command(ctx, "docker", "volume", "inspect", path)
					if err != nil {
						return item, errors.New("selected volume is unavailable")
					}
					var volumes []captureVolume
					if err = json.Unmarshal(output, &volumes); err != nil || len(volumes) != 1 {
						return item, errors.New("invalid volume inspection")
					}
					if volumes[0].Driver != "local" || len(volumes[0].Options) > 0 {
						return item, errors.New("only local Docker volumes without driver options are supported")
					}
					item.VolumeName = volumes[0].Name
					path = volumes[0].Mountpoint
				} else if source.Path != "" && filepath.Clean(source.Path) != filepath.Clean(path) {
					return item, errors.New("selected bind mount path changed; review the plan")
				}
				break
			}
		}
		if !found {
			return item, errors.New("selected mount no longer exists")
		}
		for _, container := range containers {
			for _, mount := range container.Mounts {
				if container.Config.Labels["com.docker.compose.project"] == source.StackID && container.Config.Labels["com.docker.compose.service"] == source.Service && mount.Destination == source.Mount && normalizedDataPath(mount.Source) != normalizedDataPath(path) {
					return item, errors.New("runtime mount differs from Compose; reconcile deployment before backup")
				}
				// A named volume can also be consumed through a bind mount of its
				// physical mountpoint (and vice versa). Coordinate every writer of
				// overlapping physical data regardless of the Compose mount type.
				persistentMount := mount.Type == "bind" || mount.Type == "volume"
				matching := mountKind == "volume" && mount.Type == "volume" && mount.Name == item.VolumeName || persistentMount && pathsOverlapData(path, mount.Source)
				if !matching {
					continue
				}
				item.Consumers = append(item.Consumers, Writer{ContainerID: container.ID, StackID: container.Config.Labels["com.docker.compose.project"], Service: container.Config.Labels["com.docker.compose.service"], StopTimeoutSeconds: container.Config.StopTimeout, Running: container.State.Running, ReadOnly: !mount.RW})
				if mount.RW {
					if container.State.Paused {
						return item, errors.New("a selected writer is paused; resume it before backup")
					}
					item.Writers = append(item.Writers, Writer{ContainerID: container.ID, StackID: container.Config.Labels["com.docker.compose.project"], Service: container.Config.Labels["com.docker.compose.service"], StopTimeoutSeconds: container.Config.StopTimeout, Running: container.State.Running})
				}
			}
		}
	default:
		return item, errors.New("unsupported backup source kind")
	}
	if !filepath.IsAbs(path) {
		var err error
		path, err = filepath.Abs(path)
		if err != nil {
			return item, err
		}
	}
	resolved, err := canonicalSource(path)
	if err != nil && (item.VolumeName != "" || c.cfg.BackupHelperPath != "") && errors.Is(err, os.ErrPermission) {
		resolved = filepath.Clean(path)
		err = nil
	}
	if err != nil {
		return item, err
	}
	if pathsOverlapData(resolved, c.cfg.DataDir) {
		return item, errors.New("private Stacklab state must use the Stacklab recovery scope")
	}
	item.OriginalPath = resolved
	if info, statErr := os.Stat(resolved); statErr == nil && info.Mode().IsRegular() {
		item.FileName = filepath.Base(resolved)
	}
	if source.Kind != "service_mount" {
		for _, container := range containers {
			for _, mount := range container.Mounts {
				if pathsOverlapData(resolved, mount.Source) {
					consumer := Writer{ContainerID: container.ID, StackID: container.Config.Labels["com.docker.compose.project"], Service: container.Config.Labels["com.docker.compose.service"], StopTimeoutSeconds: container.Config.StopTimeout, Running: container.State.Running, ReadOnly: !mount.RW}
					item.Consumers = append(item.Consumers, consumer)
					if !mount.RW {
						continue
					}
					if container.State.Paused {
						return item, errors.New("a selected writer is paused; resume it before backup")
					}
					item.Writers = append(item.Writers, Writer{ContainerID: container.ID, StackID: container.Config.Labels["com.docker.compose.project"], Service: container.Config.Labels["com.docker.compose.service"], StopTimeoutSeconds: container.Config.StopTimeout, Running: container.State.Running})
					break
				}
			}
		}
	}
	item.Writers = dedupeConsumers(item.Writers)
	item.Consumers = dedupeConsumers(item.Consumers)
	return item, nil
}

func dedupeConsumers(values []Writer) []Writer {
	byID := map[string]Writer{}
	for _, writer := range values {
		if previous, ok := byID[writer.ContainerID]; ok {
			writer.ReadOnly = writer.ReadOnly && previous.ReadOnly
		}
		byID[writer.ContainerID] = writer
	}
	result := make([]Writer, 0, len(byID))
	for _, writer := range byID {
		result = append(result, writer)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ContainerID < result[j].ContainerID })
	return result
}
