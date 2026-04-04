package maintenance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrDockerUnavailable = errors.New("docker unavailable")

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

		stacksUsing := make([]ImageStackUsage, 0, len(usage.stackServices))
		for stackID, serviceSet := range usage.stackServices {
			services := make([]string, 0, len(serviceSet))
			for serviceName := range serviceSet {
				services = append(services, serviceName)
			}
			sort.Strings(services)
			stacksUsing = append(stacksUsing, ImageStackUsage{
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
	output, err := s.runCommand(ctx, "docker", "ps", "-aq")
	if err != nil {
		return nil, dockerUnavailable(err)
	}

	ids := strings.Fields(strings.TrimSpace(string(output)))
	if len(ids) == 0 {
		return map[string]imageUsage{}, nil
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

	managed := make(map[string]struct{}, len(managedStackIDs))
	for _, stackID := range managedStackIDs {
		managed[stackID] = struct{}{}
	}

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

func matchesImageFilters(item ImageItem, usage ImageUsage, origin ImageOrigin, search string) bool {
	switch usage {
	case "", ImageUsageAll:
	case ImageUsageUsed:
		if item.IsUnused {
			return false
		}
	case ImageUsageUnused:
		if !item.IsUnused {
			return false
		}
	default:
		return false
	}

	switch origin {
	case "", ImageOriginAll:
	case ImageOriginStackManaged:
		if item.Source != ImageSourceStackManaged {
			return false
		}
	case ImageOriginExternal:
		if item.Source != ImageSourceExternal {
			return false
		}
	default:
		return false
	}

	if search == "" {
		return true
	}

	haystack := []string{item.Reference, item.Repository, item.Tag}
	for _, stackUsage := range item.StacksUsing {
		haystack = append(haystack, stackUsage.StackID)
		haystack = append(haystack, stackUsage.ServiceNames...)
	}
	for _, candidate := range haystack {
		if strings.Contains(strings.ToLower(candidate), search) {
			return true
		}
	}
	return false
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
