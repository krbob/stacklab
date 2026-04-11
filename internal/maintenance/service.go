package maintenance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrDockerUnavailable = errors.New("docker unavailable")
var ErrInvalidName = errors.New("invalid maintenance object name")
var ErrAlreadyExists = errors.New("maintenance object already exists")
var ErrNotFound = errors.New("maintenance object not found")
var ErrProtectedObject = errors.New("maintenance object is protected")
var ErrObjectInUse = errors.New("maintenance object is in use")

var maintenanceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var protectedNetworkNames = map[string]struct{}{
	"bridge":  {},
	"host":    {},
	"none":    {},
	"ingress": {},
}

type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

type Service struct {
	runCommand commandRunner
}

func NewService() *Service {
	return &Service{runCommand: defaultCommandRunner}
}

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func (s *Service) Images(ctx context.Context, query ImagesQuery) (ImagesResponse, error) {
	imageRows, err := s.listImages(ctx)
	if err != nil {
		return ImagesResponse{}, err
	}
	if len(imageRows) == 0 {
		return ImagesResponse{Items: []ImageItem{}}, nil
	}

	imageMeta, err := s.inspectImages(ctx, uniqueImageIDs(imageRows))
	if err != nil {
		return ImagesResponse{}, err
	}
	containerUsage, err := s.containerUsage(ctx, query.ManagedStackIDs)
	if err != nil {
		return ImagesResponse{}, err
	}

	search := strings.ToLower(strings.TrimSpace(query.Search))
	items := make([]ImageItem, 0, len(imageRows))
	for _, row := range imageRows {
		meta, ok := imageMeta[row.ID]
		if !ok {
			continue
		}
		usage := containerUsage[row.ID]

		stacksUsing := make([]StackServiceUsage, 0, len(usage.stackServices))
		for stackID, serviceSet := range usage.stackServices {
			services := make([]string, 0, len(serviceSet))
			for serviceName := range serviceSet {
				services = append(services, serviceName)
			}
			sort.Strings(services)
			stacksUsing = append(stacksUsing, StackServiceUsage{
				StackID:      stackID,
				ServiceNames: services,
			})
		}
		sort.Slice(stacksUsing, func(i, j int) bool { return stacksUsing[i].StackID < stacksUsing[j].StackID })

		source := ImageSourceExternal
		if len(stacksUsing) > 0 {
			source = ImageSourceStackManaged
		}
		item := ImageItem{
			ID:              row.ID,
			Repository:      row.Repository,
			Tag:             row.Tag,
			Reference:       row.Reference,
			SizeBytes:       meta.SizeBytes,
			CreatedAt:       meta.CreatedAt,
			ContainersUsing: usage.count,
			StacksUsing:     stacksUsing,
			IsDangling:      row.Repository == "<none>" || row.Tag == "<none>",
			IsUnused:        usage.count == 0,
			Source:          source,
		}
		if !matchesImageFilters(item, query.Usage, query.Origin, search) {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Reference == items[j].Reference {
			return items[i].ID < items[j].ID
		}
		return items[i].Reference < items[j].Reference
	})

	return ImagesResponse{Items: items}, nil
}

func (s *Service) Networks(ctx context.Context, query NetworksQuery) (NetworksResponse, error) {
	rows, err := s.listNetworks(ctx)
	if err != nil {
		return NetworksResponse{}, err
	}
	if len(rows) == 0 {
		return NetworksResponse{Items: []NetworkItem{}}, nil
	}

	meta, err := s.inspectNetworks(ctx, uniqueNetworkIDs(rows))
	if err != nil {
		return NetworksResponse{}, err
	}
	usage, err := s.objectUsage(ctx, query.ManagedStackIDs)
	if err != nil {
		return NetworksResponse{}, err
	}

	search := strings.ToLower(strings.TrimSpace(query.Search))
	items := make([]NetworkItem, 0, len(rows))
	for _, row := range rows {
		inspected, ok := meta[row.ID]
		if !ok {
			continue
		}
		networkUsage := usage.networks[row.Name]
		stacksUsing := mergeStackServiceUsage(usageMapToStackServiceUsage(networkUsage.stackServices), labelsToStackUsage(inspected.Labels, query.ManagedStackIDs))
		source := NetworkSourceExternal
		if len(stacksUsing) > 0 {
			source = NetworkSourceStackManaged
		}
		item := NetworkItem{
			ID:              row.ID,
			Name:            row.Name,
			Driver:          row.Driver,
			Scope:           row.Scope,
			Internal:        inspected.Internal,
			Attachable:      inspected.Attachable,
			Ingress:         inspected.Ingress,
			ContainersUsing: networkUsage.count,
			StacksUsing:     stacksUsing,
			IsUnused:        networkUsage.count == 0,
			Source:          source,
		}
		if !matchesInventoryFilters(item.IsUnused, string(item.Source), search, append([]string{item.Name, item.Driver}, stackUsageHaystack(stacksUsing)...)...) {
			continue
		}
		if !matchesUsageOrigin(query.Usage, query.Origin, item.IsUnused, string(item.Source)) {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return NetworksResponse{Items: items}, nil
}

func (s *Service) Volumes(ctx context.Context, query VolumesQuery) (VolumesResponse, error) {
	rows, err := s.listVolumes(ctx)
	if err != nil {
		return VolumesResponse{}, err
	}
	if len(rows) == 0 {
		return VolumesResponse{Items: []VolumeItem{}}, nil
	}

	meta, err := s.inspectVolumes(ctx, uniqueVolumeNames(rows))
	if err != nil {
		return VolumesResponse{}, err
	}
	usage, err := s.objectUsage(ctx, query.ManagedStackIDs)
	if err != nil {
		return VolumesResponse{}, err
	}

	search := strings.ToLower(strings.TrimSpace(query.Search))
	items := make([]VolumeItem, 0, len(rows))
	for _, row := range rows {
		inspected, ok := meta[row.Name]
		if !ok {
			continue
		}
		volumeUsage := usage.volumes[row.Name]
		stacksUsing := mergeStackServiceUsage(usageMapToStackServiceUsage(volumeUsage.stackServices), labelsToStackUsage(inspected.Labels, query.ManagedStackIDs))
		source := VolumeSourceExternal
		if len(stacksUsing) > 0 {
			source = VolumeSourceStackManaged
		}
		item := VolumeItem{
			Name:            row.Name,
			Driver:          row.Driver,
			Mountpoint:      inspected.Mountpoint,
			Scope:           inspected.Scope,
			OptionsCount:    len(inspected.Options),
			ContainersUsing: volumeUsage.count,
			StacksUsing:     stacksUsing,
			IsUnused:        volumeUsage.count == 0,
			Source:          source,
		}
		if !matchesInventoryFilters(item.IsUnused, string(item.Source), search, append([]string{item.Name, item.Driver, item.Mountpoint}, stackUsageHaystack(stacksUsing)...)...) {
			continue
		}
		if !matchesUsageOrigin(query.Usage, query.Origin, item.IsUnused, string(item.Source)) {
			continue
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].Mountpoint < items[j].Mountpoint
		}
		return items[i].Name < items[j].Name
	})
	return VolumesResponse{Items: items}, nil
}

func (s *Service) CreateNetwork(ctx context.Context, request CreateNetworkRequest) (CreateNetworkResponse, error) {
	name := strings.TrimSpace(request.Name)
	if !isValidMaintenanceObjectName(name) {
		return CreateNetworkResponse{}, ErrInvalidName
	}

	rows, err := s.listNetworks(ctx)
	if err != nil {
		return CreateNetworkResponse{}, err
	}
	for _, row := range rows {
		if row.Name == name {
			return CreateNetworkResponse{}, ErrAlreadyExists
		}
	}

	if _, err := s.runCommand(ctx, "docker", "network", "create", name); err != nil {
		return CreateNetworkResponse{}, dockerMutationError(err)
	}

	return CreateNetworkResponse{
		Created: true,
		Name:    name,
	}, nil
}

func (s *Service) DeleteNetwork(ctx context.Context, name string, managedStackIDs []string) (DeleteNetworkResponse, error) {
	name = strings.TrimSpace(name)
	if !isValidMaintenanceObjectName(name) {
		return DeleteNetworkResponse{}, ErrInvalidName
	}
	if _, protected := protectedNetworkNames[name]; protected {
		return DeleteNetworkResponse{}, ErrProtectedObject
	}

	item, err := s.networkByName(ctx, name, managedStackIDs)
	if err != nil {
		return DeleteNetworkResponse{}, err
	}
	if item.Source != NetworkSourceExternal {
		return DeleteNetworkResponse{}, ErrProtectedObject
	}
	if !item.IsUnused {
		return DeleteNetworkResponse{}, ErrObjectInUse
	}

	if _, err := s.runCommand(ctx, "docker", "network", "rm", name); err != nil {
		return DeleteNetworkResponse{}, dockerMutationError(err)
	}

	return DeleteNetworkResponse{
		Deleted: true,
		Name:    name,
	}, nil
}

func (s *Service) CreateVolume(ctx context.Context, request CreateVolumeRequest) (CreateVolumeResponse, error) {
	name := strings.TrimSpace(request.Name)
	if !isValidMaintenanceObjectName(name) {
		return CreateVolumeResponse{}, ErrInvalidName
	}

	rows, err := s.listVolumes(ctx)
	if err != nil {
		return CreateVolumeResponse{}, err
	}
	for _, row := range rows {
		if row.Name == name {
			return CreateVolumeResponse{}, ErrAlreadyExists
		}
	}

	if _, err := s.runCommand(ctx, "docker", "volume", "create", name); err != nil {
		return CreateVolumeResponse{}, dockerMutationError(err)
	}

	return CreateVolumeResponse{
		Created: true,
		Name:    name,
	}, nil
}

func (s *Service) DeleteVolume(ctx context.Context, name string, managedStackIDs []string) (DeleteVolumeResponse, error) {
	name = strings.TrimSpace(name)
	if !isValidMaintenanceObjectName(name) {
		return DeleteVolumeResponse{}, ErrInvalidName
	}

	item, err := s.volumeByName(ctx, name, managedStackIDs)
	if err != nil {
		return DeleteVolumeResponse{}, err
	}
	if item.Source != VolumeSourceExternal {
		return DeleteVolumeResponse{}, ErrProtectedObject
	}
	if !item.IsUnused {
		return DeleteVolumeResponse{}, ErrObjectInUse
	}

	if _, err := s.runCommand(ctx, "docker", "volume", "rm", name); err != nil {
		return DeleteVolumeResponse{}, dockerMutationError(err)
	}

	return DeleteVolumeResponse{
		Deleted: true,
		Name:    name,
	}, nil
}

func (s *Service) PrunePreview(ctx context.Context, query PrunePreviewQuery) (PrunePreviewResponse, error) {
	systemDF, err := s.systemDF(ctx)
	if err != nil {
		return PrunePreviewResponse{}, err
	}

	preview := PrunePreview{}
	if query.Images {
		images, err := s.Images(ctx, ImagesQuery{
			Usage:           ImageUsageUnused,
			Origin:          ImageOriginAll,
			ManagedStackIDs: query.ManagedStackIDs,
		})
		if err != nil {
			return PrunePreviewResponse{}, err
		}
		preview.Images = buildImagePrunePreview(images.Items)
	}
	if query.BuildCache {
		preview.BuildCache = previewFromSystemDF(systemDF["build cache"])
	}
	if query.StoppedContainers {
		containersPreview := previewFromSystemDF(systemDF["containers"])
		active := systemDF["containers"].Active
		if containersPreview.Count > active {
			containersPreview.Count -= active
		} else {
			containersPreview.Count = 0
		}
		preview.StoppedContainers = containersPreview
	}
	if query.Volumes {
		preview.Volumes = previewFromSystemDF(systemDF["local volumes"])
	}

	preview.TotalReclaimableBytes =
		preview.Images.ReclaimableBytes +
			preview.BuildCache.ReclaimableBytes +
			preview.StoppedContainers.ReclaimableBytes +
			preview.Volumes.ReclaimableBytes

	return PrunePreviewResponse{Preview: preview}, nil
}

func (s *Service) RunPruneStep(ctx context.Context, action string) (string, error) {
	var args []string
	switch action {
	case "prune_images":
		args = []string{"image", "prune", "-af"}
	case "prune_build_cache":
		args = []string{"builder", "prune", "-af"}
	case "prune_stopped_containers":
		args = []string{"container", "prune", "-f"}
	case "prune_volumes":
		args = []string{"volume", "prune", "-f"}
	default:
		return "", fmt.Errorf("unsupported prune action: %s", action)
	}

	output, err := s.runCommand(ctx, "docker", args...)
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			trimmed = err.Error()
		}
		return trimmed, errors.New(trimmed)
	}
	return trimmed, nil
}

func (s *Service) RunSystemPrune(ctx context.Context, includeVolumes bool) (string, error) {
	args := []string{"system", "prune", "-af"}
	if includeVolumes {
		args = append(args, "--volumes")
	}
	output, err := s.runCommand(ctx, "docker", args...)
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		if trimmed == "" {
			trimmed = err.Error()
		}
		return trimmed, errors.New(trimmed)
	}
	return trimmed, nil
}

func (s *Service) networkByName(ctx context.Context, name string, managedStackIDs []string) (NetworkItem, error) {
	networks, err := s.Networks(ctx, NetworksQuery{
		Usage:           ImageUsageAll,
		Origin:          ImageOriginAll,
		ManagedStackIDs: managedStackIDs,
	})
	if err != nil {
		return NetworkItem{}, err
	}
	for _, item := range networks.Items {
		if item.Name == name {
			return item, nil
		}
	}
	return NetworkItem{}, ErrNotFound
}

func (s *Service) volumeByName(ctx context.Context, name string, managedStackIDs []string) (VolumeItem, error) {
	volumes, err := s.Volumes(ctx, VolumesQuery{
		Usage:           ImageUsageAll,
		Origin:          ImageOriginAll,
		ManagedStackIDs: managedStackIDs,
	})
	if err != nil {
		return VolumeItem{}, err
	}
	for _, item := range volumes.Items {
		if item.Name == name {
			return item, nil
		}
	}
	return VolumeItem{}, ErrNotFound
}

func isValidMaintenanceObjectName(value string) bool {
	return maintenanceNamePattern.MatchString(value)
}

func dockerMutationError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "executable file not found"), strings.Contains(message, "command not found"):
		return dockerUnavailable(err)
	case strings.Contains(message, "already exists"):
		return ErrAlreadyExists
	case strings.Contains(message, "not found"), strings.Contains(message, "no such network"), strings.Contains(message, "no such volume"):
		return ErrNotFound
	case strings.Contains(message, "is in use"), strings.Contains(message, "has active endpoints"), strings.Contains(message, "volume is in use"):
		return ErrObjectInUse
	default:
		return fmt.Errorf("docker mutation failed: %w", err)
	}
}

type imageRow struct {
	ID         string
	Repository string
	Tag        string
	Reference  string
}

type imageMeta struct {
	ID        string
	CreatedAt time.Time
	SizeBytes int64
}

type imageUsage struct {
	count         int
	stackServices map[string]map[string]struct{}
}

type objectUsage struct {
	networks map[string]usageByObject
	volumes  map[string]usageByObject
}

type usageByObject struct {
	count         int
	stackServices map[string]map[string]struct{}
}

type dockerImageInspect struct {
	ID       string   `json:"Id"`
	Created  string   `json:"Created"`
	Size     int64    `json:"Size"`
	RepoTags []string `json:"RepoTags"`
}

type dockerContainerInspect struct {
	Image  string `json:"Image"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Name string `json:"Name"`
		Type string `json:"Type"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct{} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type networkRow struct {
	ID     string
	Name   string
	Driver string
	Scope  string
}

type dockerNetworkInspect struct {
	ID         string            `json:"Id"`
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Scope      string            `json:"Scope"`
	Internal   bool              `json:"Internal"`
	Attachable bool              `json:"Attachable"`
	Ingress    bool              `json:"Ingress"`
	Labels     map[string]string `json:"Labels"`
}

type volumeRow struct {
	Name   string
	Driver string
}

type dockerVolumeInspect struct {
	Name       string            `json:"Name"`
	Driver     string            `json:"Driver"`
	Mountpoint string            `json:"Mountpoint"`
	Scope      string            `json:"Scope"`
	Labels     map[string]string `json:"Labels"`
	Options    map[string]string `json:"Options"`
}

type systemDFRow struct {
	Type        string `json:"Type"`
	TotalCount  string `json:"TotalCount"`
	Active      string `json:"Active"`
	Size        string `json:"Size"`
	Reclaimable string `json:"Reclaimable"`
}

type systemDFSummary struct {
	TotalCount       int
	Active           int
	SizeBytes        int64
	ReclaimableBytes int64
}

func (s *Service) listImages(ctx context.Context) ([]imageRow, error) {
	output, err := s.runCommand(ctx, "docker", "image", "ls", "--all", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	rows := make([]imageRow, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID         string `json:"ID"`
			Repository string `json:"Repository"`
			Tag        string `json:"Tag"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse docker image ls output: %w", err)
		}
		reference := raw.Repository
		if raw.Tag != "" {
			reference += ":" + raw.Tag
		}
		rows = append(rows, imageRow{
			ID:         raw.ID,
			Repository: raw.Repository,
			Tag:        raw.Tag,
			Reference:  reference,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan docker image ls output: %w", err)
	}
	return rows, nil
}

func uniqueImageIDs(rows []imageRow) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		ids = append(ids, row.ID)
	}
	return ids
}

func (s *Service) inspectImages(ctx context.Context, ids []string) (map[string]imageMeta, error) {
	if len(ids) == 0 {
		return map[string]imageMeta{}, nil
	}
	args := append([]string{"image", "inspect"}, ids...)
	output, err := s.runCommand(ctx, "docker", args...)
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	var inspected []dockerImageInspect
	if err := json.Unmarshal(output, &inspected); err != nil {
		return nil, fmt.Errorf("parse docker image inspect output: %w", err)
	}

	result := make(map[string]imageMeta, len(inspected))
	for _, item := range inspected {
		createdAt, _ := time.Parse(time.RFC3339Nano, item.Created)
		result[item.ID] = imageMeta{
			ID:        item.ID,
			CreatedAt: createdAt.UTC(),
			SizeBytes: item.Size,
		}
	}
	return result, nil
}

func (s *Service) containerUsage(ctx context.Context, managedStackIDs []string) (map[string]imageUsage, error) {
	containers, err := s.inspectAllContainers(ctx)
	if err != nil {
		return nil, err
	}

	managed := managedStackIDSet(managedStackIDs)
	result := make(map[string]imageUsage)
	for _, container := range containers {
		imageID := container.Image
		usage := result[imageID]
		usage.count++
		if usage.stackServices == nil {
			usage.stackServices = map[string]map[string]struct{}{}
		}
		project := container.Config.Labels["com.docker.compose.project"]
		serviceName := container.Config.Labels["com.docker.compose.service"]
		if _, ok := managed[project]; ok && serviceName != "" {
			serviceSet := usage.stackServices[project]
			if serviceSet == nil {
				serviceSet = map[string]struct{}{}
				usage.stackServices[project] = serviceSet
			}
			serviceSet[serviceName] = struct{}{}
		}
		result[imageID] = usage
	}
	return result, nil
}

func (s *Service) objectUsage(ctx context.Context, managedStackIDs []string) (objectUsage, error) {
	containers, err := s.inspectAllContainers(ctx)
	if err != nil {
		return objectUsage{}, err
	}

	managed := managedStackIDSet(managedStackIDs)
	result := objectUsage{
		networks: map[string]usageByObject{},
		volumes:  map[string]usageByObject{},
	}
	for _, container := range containers {
		project := container.Config.Labels["com.docker.compose.project"]
		serviceName := container.Config.Labels["com.docker.compose.service"]
		_, managedProject := managed[project]
		managedService := managedProject && serviceName != ""
		for networkName := range container.NetworkSettings.Networks {
			usage := result.networks[networkName]
			usage.count++
			if managedService {
				addUsageStackService(&usage, project, serviceName)
			}
			result.networks[networkName] = usage
		}
		for _, mount := range container.Mounts {
			if mount.Type != "volume" || mount.Name == "" {
				continue
			}
			usage := result.volumes[mount.Name]
			usage.count++
			if managedService {
				addUsageStackService(&usage, project, serviceName)
			}
			result.volumes[mount.Name] = usage
		}
	}
	return result, nil
}

func (s *Service) inspectAllContainers(ctx context.Context) ([]dockerContainerInspect, error) {
	output, err := s.runCommand(ctx, "docker", "ps", "-aq")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	ids := strings.Fields(strings.TrimSpace(string(output)))
	if len(ids) == 0 {
		return []dockerContainerInspect{}, nil
	}

	args := append([]string{"inspect"}, ids...)
	inspectOutput, err := s.runCommand(ctx, "docker", args...)
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	var containers []dockerContainerInspect
	if err := json.Unmarshal(inspectOutput, &containers); err != nil {
		return nil, fmt.Errorf("parse docker inspect output: %w", err)
	}

	return containers, nil
}

func (s *Service) listNetworks(ctx context.Context) ([]networkRow, error) {
	output, err := s.runCommand(ctx, "docker", "network", "ls", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	rows := make([]networkRow, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			ID     string `json:"ID"`
			Name   string `json:"Name"`
			Driver string `json:"Driver"`
			Scope  string `json:"Scope"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse docker network ls output: %w", err)
		}
		rows = append(rows, networkRow{
			ID:     raw.ID,
			Name:   raw.Name,
			Driver: raw.Driver,
			Scope:  raw.Scope,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan docker network ls output: %w", err)
	}
	return rows, nil
}

func uniqueNetworkIDs(rows []networkRow) []string {
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.ID]; ok {
			continue
		}
		seen[row.ID] = struct{}{}
		ids = append(ids, row.ID)
	}
	return ids
}

func (s *Service) inspectNetworks(ctx context.Context, ids []string) (map[string]dockerNetworkInspect, error) {
	if len(ids) == 0 {
		return map[string]dockerNetworkInspect{}, nil
	}
	args := append([]string{"network", "inspect"}, ids...)
	output, err := s.runCommand(ctx, "docker", args...)
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	var inspected []dockerNetworkInspect
	if err := json.Unmarshal(output, &inspected); err != nil {
		return nil, fmt.Errorf("parse docker network inspect output: %w", err)
	}

	result := make(map[string]dockerNetworkInspect, len(inspected))
	for _, item := range inspected {
		result[item.ID] = item
	}
	return result, nil
}

func (s *Service) listVolumes(ctx context.Context) ([]volumeRow, error) {
	output, err := s.runCommand(ctx, "docker", "volume", "ls", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	rows := make([]volumeRow, 0)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw struct {
			Name   string `json:"Name"`
			Driver string `json:"Driver"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("parse docker volume ls output: %w", err)
		}
		rows = append(rows, volumeRow{
			Name:   raw.Name,
			Driver: raw.Driver,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan docker volume ls output: %w", err)
	}
	return rows, nil
}

func uniqueVolumeNames(rows []volumeRow) []string {
	seen := map[string]struct{}{}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.Name]; ok {
			continue
		}
		seen[row.Name] = struct{}{}
		names = append(names, row.Name)
	}
	return names
}

func (s *Service) inspectVolumes(ctx context.Context, names []string) (map[string]dockerVolumeInspect, error) {
	if len(names) == 0 {
		return map[string]dockerVolumeInspect{}, nil
	}
	args := append([]string{"volume", "inspect"}, names...)
	output, err := s.runCommand(ctx, "docker", args...)
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	var inspected []dockerVolumeInspect
	if err := json.Unmarshal(output, &inspected); err != nil {
		return nil, fmt.Errorf("parse docker volume inspect output: %w", err)
	}

	result := make(map[string]dockerVolumeInspect, len(inspected))
	for _, item := range inspected {
		result[item.Name] = item
	}
	return result, nil
}

func matchesImageFilters(item ImageItem, usage ImageUsage, origin ImageOrigin, search string) bool {
	if !matchesUsageOrigin(usage, origin, item.IsUnused, string(item.Source)) {
		return false
	}
	return matchesInventoryFilters(item.IsUnused, string(item.Source), search, append([]string{item.Reference, item.Repository, item.Tag}, stackUsageHaystack(item.StacksUsing)...)...)
}

func matchesUsageOrigin(usage ImageUsage, origin ImageOrigin, isUnused bool, source string) bool {
	switch usage {
	case "", ImageUsageAll:
	case ImageUsageUsed:
		if isUnused {
			return false
		}
	case ImageUsageUnused:
		if !isUnused {
			return false
		}
	default:
		return false
	}

	switch origin {
	case "", ImageOriginAll:
	case ImageOriginStackManaged:
		if source != string(ImageSourceStackManaged) {
			return false
		}
	case ImageOriginExternal:
		if source != string(ImageSourceExternal) {
			return false
		}
	default:
		return false
	}

	return true
}

func matchesInventoryFilters(_ bool, _ string, search string, primary ...string) bool {
	if search == "" {
		return true
	}

	for _, candidate := range primary {
		if strings.Contains(strings.ToLower(candidate), search) {
			return true
		}
	}
	return false
}

func stackUsageHaystack(items []StackServiceUsage) []string {
	haystack := make([]string, 0, len(items)*2)
	for _, stackUsage := range items {
		haystack = append(haystack, stackUsage.StackID)
		haystack = append(haystack, stackUsage.ServiceNames...)
	}
	return haystack
}

func managedStackIDSet(managedStackIDs []string) map[string]struct{} {
	managed := make(map[string]struct{}, len(managedStackIDs))
	for _, stackID := range managedStackIDs {
		managed[stackID] = struct{}{}
	}
	return managed
}

func addUsageStackService(usage *usageByObject, stackID, serviceName string) {
	if usage.stackServices == nil {
		usage.stackServices = map[string]map[string]struct{}{}
	}
	serviceSet := usage.stackServices[stackID]
	if serviceSet == nil {
		serviceSet = map[string]struct{}{}
		usage.stackServices[stackID] = serviceSet
	}
	serviceSet[serviceName] = struct{}{}
}

func labelsToStackUsage(labels map[string]string, managedStackIDs []string) []StackServiceUsage {
	if len(labels) == 0 {
		return nil
	}
	project := strings.TrimSpace(labels["com.docker.compose.project"])
	if project == "" {
		return nil
	}
	managed := managedStackIDSet(managedStackIDs)
	if _, ok := managed[project]; !ok {
		return nil
	}
	return []StackServiceUsage{{StackID: project, ServiceNames: nil}}
}

func mergeStackServiceUsage(usages ...[]StackServiceUsage) []StackServiceUsage {
	merged := map[string]map[string]struct{}{}
	for _, group := range usages {
		for _, item := range group {
			serviceSet := merged[item.StackID]
			if serviceSet == nil {
				serviceSet = map[string]struct{}{}
				merged[item.StackID] = serviceSet
			}
			for _, serviceName := range item.ServiceNames {
				serviceSet[serviceName] = struct{}{}
			}
		}
	}
	result := make([]StackServiceUsage, 0, len(merged))
	for stackID, serviceSet := range merged {
		services := make([]string, 0, len(serviceSet))
		for serviceName := range serviceSet {
			services = append(services, serviceName)
		}
		sort.Strings(services)
		result = append(result, StackServiceUsage{StackID: stackID, ServiceNames: services})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StackID < result[j].StackID })
	return result
}

func usageMapToStackServiceUsage(items map[string]map[string]struct{}) []StackServiceUsage {
	if len(items) == 0 {
		return nil
	}
	result := make([]StackServiceUsage, 0, len(items))
	for stackID, serviceSet := range items {
		services := make([]string, 0, len(serviceSet))
		for serviceName := range serviceSet {
			services = append(services, serviceName)
		}
		sort.Strings(services)
		result = append(result, StackServiceUsage{StackID: stackID, ServiceNames: services})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].StackID < result[j].StackID })
	return result
}

func buildImagePrunePreview(items []ImageItem) PruneCategoryPreview {
	seen := map[string]struct{}{}
	previewItems := make([]PrunePreviewItem, 0)
	var total int64
	for _, item := range items {
		if _, ok := seen[item.ID]; ok {
			continue
		}
		seen[item.ID] = struct{}{}
		previewItems = append(previewItems, PrunePreviewItem{
			Reference: item.Reference,
			SizeBytes: item.SizeBytes,
			Reason:    "unused_image",
		})
		total += item.SizeBytes
	}
	sort.Slice(previewItems, func(i, j int) bool { return previewItems[i].Reference < previewItems[j].Reference })
	return PruneCategoryPreview{
		Count:            len(previewItems),
		ReclaimableBytes: total,
		Items:            previewItems,
	}
}

func (s *Service) systemDF(ctx context.Context) (map[string]systemDFSummary, error) {
	output, err := s.runCommand(ctx, "docker", "system", "df", "--format", "{{json .}}")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	summaries := map[string]systemDFSummary{}
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var row systemDFRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker system df output: %w", err)
		}
		totalCount, _ := strconv.Atoi(strings.TrimSpace(row.TotalCount))
		active, _ := strconv.Atoi(strings.TrimSpace(row.Active))
		sizeBytes, _ := parseDockerSize(row.Size)
		reclaimableBytes, _ := parseDockerReclaimable(row.Reclaimable)
		summaries[strings.ToLower(strings.TrimSpace(row.Type))] = systemDFSummary{
			TotalCount:       totalCount,
			Active:           active,
			SizeBytes:        sizeBytes,
			ReclaimableBytes: reclaimableBytes,
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan docker system df output: %w", err)
	}
	return summaries, nil
}

func previewFromSystemDF(summary systemDFSummary) PruneCategoryPreview {
	return PruneCategoryPreview{
		Count:            summary.TotalCount,
		ReclaimableBytes: summary.ReclaimableBytes,
	}
}

func parseDockerReclaimable(value string) (int64, error) {
	base := strings.TrimSpace(value)
	if idx := strings.Index(base, "("); idx >= 0 {
		base = strings.TrimSpace(base[:idx])
	}
	return parseDockerSize(base)
}

func parseDockerSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0" || trimmed == "0B" {
		return 0, nil
	}
	upper := strings.ToUpper(trimmed)
	units := []string{"KB", "MB", "GB", "TB", "PB", "B"}
	multipliers := map[string]float64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
		"PB": 1024 * 1024 * 1024 * 1024 * 1024,
	}
	for _, unit := range units {
		if strings.HasSuffix(upper, unit) {
			number := strings.TrimSpace(upper[:len(upper)-len(unit)])
			parsed, err := strconv.ParseFloat(number, 64)
			if err != nil {
				return 0, err
			}
			return int64(parsed * multipliers[unit]), nil
		}
	}
	return 0, fmt.Errorf("unsupported size value: %s", value)
}

func dockerUnavailable(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrDockerUnavailable, err)
}
