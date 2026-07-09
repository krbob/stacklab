package configworkspace

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"stacklab/internal/atomicfile"
	"stacklab/internal/config"
	"stacklab/internal/fsmeta"
	"stacklab/internal/stacks"
	"stacklab/internal/workspacerepair"
)

var (
	ErrNotFound             = errors.New("config workspace path not found")
	ErrPathOutsideWorkspace = errors.New("path outside config workspace")
	ErrPathNotDirectory     = errors.New("path is not a directory")
	ErrPathNotFile          = errors.New("path is not a file")
	ErrBinaryNotEditable    = errors.New("binary file is not editable")
	ErrPermissionDenied     = errors.New("config workspace permission denied")
)

type Service struct {
	repoRoot      string
	workspaceRoot string
	repairer      permissionRepairer
}

type permissionRepairer interface {
	Capability(ctx context.Context) workspacerepair.Capability
	Repair(ctx context.Context, targetPath string, recursive bool) (workspacerepair.Result, error)
}

func NewService(cfg config.Config) *Service {
	repoRoot := cfg.RootDir
	if absolute, err := filepath.Abs(repoRoot); err == nil {
		repoRoot = absolute
	}
	root := filepath.Join(repoRoot, "config")
	if absolute, err := filepath.Abs(root); err == nil {
		root = absolute
	}
	return &Service{
		repoRoot:      repoRoot,
		workspaceRoot: root,
		repairer:      workspacerepair.NewService(cfg),
	}
}

func (s *Service) Tree(ctx context.Context, currentPath string) (TreeResponse, error) {
	if err := os.MkdirAll(s.workspaceRoot, 0o755); err != nil {
		return TreeResponse{}, fmt.Errorf("create config workspace root: %w", err)
	}

	normalized, err := normalizeRelativePath(currentPath)
	if err != nil {
		return TreeResponse{}, err
	}

	resolvedPath, err := s.resolveExistingPath(normalized)
	if err != nil {
		return TreeResponse{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return TreeResponse{}, ErrNotFound
		}
		return TreeResponse{}, fmt.Errorf("stat config workspace directory: %w", err)
	}
	if !info.IsDir() {
		return TreeResponse{}, ErrPathNotDirectory
	}

	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return TreeResponse{}, ErrPermissionDenied
		}
		return TreeResponse{}, fmt.Errorf("read config workspace directory: %w", err)
	}

	items := make([]TreeEntry, 0, len(entries))
	for _, entry := range entries {
		childPath := joinRelativePath(normalized, entry.Name())
		childResolved, err := s.resolveExistingPath(childPath)
		if err != nil {
			if errors.Is(err, ErrPathOutsideWorkspace) || errors.Is(err, ErrNotFound) {
				continue
			}
			return TreeResponse{}, err
		}

		childInfo, err := os.Stat(childResolved)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return TreeResponse{}, fmt.Errorf("stat config workspace entry: %w", err)
		}

		entryType, err := detectEntryType(childResolved, childInfo)
		if err != nil {
			return TreeResponse{}, err
		}
		permissions := fsmeta.Inspect(childResolved, childInfo)

		sizeBytes := childInfo.Size()
		if childInfo.IsDir() {
			sizeBytes = 0
		}

		items = append(items, TreeEntry{
			Name:        entry.Name(),
			Path:        childPath,
			Type:        entryType,
			SizeBytes:   sizeBytes,
			ModifiedAt:  childInfo.ModTime().UTC(),
			StackID:     deriveStackID(childPath),
			Permissions: permissions,
		})
	}
	s.markGitIgnored(ctx, items)

	sort.Slice(items, func(i, j int) bool {
		leftDir := items[i].Type == EntryTypeDirectory
		rightDir := items[j].Type == EntryTypeDirectory
		switch {
		case leftDir != rightDir:
			return leftDir
		default:
			return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
		}
	})

	response := TreeResponse{
		WorkspaceRoot: s.workspaceRoot,
		CurrentPath:   normalized,
		Items:         items,
	}
	if normalized != "" {
		parent := parentRelativePath(normalized)
		response.ParentPath = &parent
	}

	return response, nil
}

func (s *Service) File(ctx context.Context, filePath string) (FileResponse, error) {
	normalized, err := normalizeRequiredFilePath(filePath)
	if err != nil {
		return FileResponse{}, err
	}

	resolvedPath, err := s.resolveExistingPath(normalized)
	if err != nil {
		return FileResponse{}, err
	}

	info, err := os.Stat(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return FileResponse{}, ErrNotFound
		}
		return FileResponse{}, fmt.Errorf("stat config workspace file: %w", err)
	}
	if info.IsDir() {
		return FileResponse{}, ErrPathNotFile
	}

	entryType, err := detectEntryType(resolvedPath, info)
	if err != nil {
		return FileResponse{}, err
	}
	permissions := fsmeta.Inspect(resolvedPath, info)
	readable := permissions.Readable
	blockedReason := configBlockedReason(readable, permissions.Writable, entryType)

	response := FileResponse{
		Path:             normalized,
		Name:             path.Base(normalized),
		Type:             entryType,
		StackID:          deriveStackID(normalized),
		SizeBytes:        info.Size(),
		ModifiedAt:       info.ModTime().UTC(),
		GitIgnored:       s.isGitIgnored(ctx, normalized, false),
		Readable:         readable,
		Writable:         entryType == EntryTypeTextFile && readable && permissions.Writable,
		BlockedReason:    blockedReason,
		Permissions:      permissions,
		RepairCapability: s.repairer.Capability(ctx),
	}

	if entryType == EntryTypeTextFile && readable {
		contentBytes, err := os.ReadFile(resolvedPath)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				reason := "not_readable"
				response.Readable = false
				response.Writable = false
				response.BlockedReason = &reason
				return response, nil
			}
			return FileResponse{}, fmt.Errorf("read config workspace file: %w", err)
		}
		content := string(contentBytes)
		encoding := "utf-8"
		response.Content = &content
		response.Encoding = &encoding
		return response, nil
	}

	response.Writable = false
	return response, nil
}

func (s *Service) markGitIgnored(ctx context.Context, items []TreeEntry) {
	ignored := s.gitIgnoredPaths(ctx, items)
	for index := range items {
		items[index].GitIgnored = ignored[items[index].Path]
	}
}

func (s *Service) isGitIgnored(ctx context.Context, relativePath string, isDir bool) bool {
	entryType := EntryTypeTextFile
	if isDir {
		entryType = EntryTypeDirectory
	}
	ignored := s.gitIgnoredPaths(ctx, []TreeEntry{{
		Path: relativePath,
		Type: entryType,
	}})
	return ignored[relativePath]
}

func (s *Service) gitIgnoredPaths(ctx context.Context, items []TreeEntry) map[string]bool {
	ignored := make(map[string]bool)
	if len(items) == 0 || strings.TrimSpace(s.repoRoot) == "" {
		return ignored
	}

	repoToConfigPath := make(map[string]string, len(items))
	var stdin strings.Builder
	for _, item := range items {
		if item.Path == "" {
			continue
		}
		repoPath := path.Join("config", item.Path)
		repoToConfigPath[repoPath] = item.Path
		stdin.WriteString(repoPath)
		stdin.WriteByte('\n')
		if item.Type == EntryTypeDirectory {
			dirPath := repoPath + "/"
			repoToConfigPath[dirPath] = item.Path
			stdin.WriteString(dirPath)
			stdin.WriteByte('\n')
		}
	}
	if len(repoToConfigPath) == 0 {
		return ignored
	}

	command := exec.CommandContext(ctx, "git", "-C", s.repoRoot, "check-ignore", "--stdin")
	command.Stdin = strings.NewReader(stdin.String())
	output, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return ignored
		}
		return ignored
	}

	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		if relativePath, ok := repoToConfigPath[line]; ok {
			ignored[relativePath] = true
			continue
		}
		if relativePath, ok := repoToConfigPath[strings.TrimSuffix(line, "/")]; ok {
			ignored[relativePath] = true
		}
	}
	return ignored
}

func (s *Service) SaveFile(ctx context.Context, request SaveFileRequest) (SaveFileResponse, error) {
	_ = ctx

	normalized, err := normalizeRequiredFilePath(request.Path)
	if err != nil {
		return SaveFileResponse{}, err
	}

	targetPath, err := s.resolveSaveTarget(normalized, request.CreateParentDirectories)
	if err != nil {
		return SaveFileResponse{}, err
	}

	if err := writeFileAtomic(targetPath, request.Content); err != nil {
		return SaveFileResponse{}, fmt.Errorf("write config workspace file: %w", err)
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		return SaveFileResponse{}, fmt.Errorf("stat saved config workspace file: %w", err)
	}

	return SaveFileResponse{
		Saved:       true,
		Path:        normalized,
		ModifiedAt:  info.ModTime().UTC(),
		AuditAction: "save_config_file",
	}, nil
}

func (s *Service) RepairPermissions(ctx context.Context, request RepairPermissionsRequest) (RepairPermissionsResponse, error) {
	normalized, err := normalizeRequiredRepairPath(request.Path)
	if err != nil {
		return RepairPermissionsResponse{}, err
	}

	targetPath, err := s.resolveExistingPath(normalized)
	if err != nil {
		return RepairPermissionsResponse{}, err
	}

	result, err := s.repairer.Repair(ctx, targetPath, request.Recursive)
	if err != nil {
		return RepairPermissionsResponse{}, err
	}

	return RepairPermissionsResponse{
		Repaired:                true,
		Path:                    normalized,
		Recursive:               request.Recursive,
		ChangedItems:            result.ChangedItems,
		Warnings:                append([]string(nil), result.Warnings...),
		TargetPermissionsBefore: result.TargetPermissionsBefore,
		TargetPermissionsAfter:  result.TargetPermissionsAfter,
		AuditAction:             "repair_config_workspace_permissions",
		RepairCapability:        s.repairer.Capability(ctx),
	}, nil
}

func (s *Service) resolveSaveTarget(normalizedPath string, createParentDirectories bool) (string, error) {
	if existingPath, err := s.resolveExistingPath(normalizedPath); err == nil {
		info, statErr := os.Stat(existingPath)
		if statErr != nil {
			return "", fmt.Errorf("stat config workspace path: %w", statErr)
		}
		if info.IsDir() {
			return "", ErrPathNotFile
		}
		permissions := fsmeta.Inspect(existingPath, info)
		if !permissions.Readable || !permissions.Writable {
			return "", ErrPermissionDenied
		}

		entryType, detectErr := detectEntryType(existingPath, info)
		if detectErr != nil {
			return "", detectErr
		}
		if entryType != EntryTypeTextFile {
			return "", ErrBinaryNotEditable
		}
		return existingPath, nil
	} else if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	if err := os.MkdirAll(s.workspaceRoot, 0o755); err != nil {
		return "", fmt.Errorf("create config workspace root: %w", err)
	}

	parentPath := parentRelativePath(normalizedPath)
	parentResolved, err := s.resolveExistingPath(parentPath)
	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound) && createParentDirectories:
		parentResolved = filepath.Join(s.workspaceRoot, filepath.FromSlash(parentPath))
		if err := s.ensureWithinRoot(parentResolved); err != nil {
			return "", err
		}
		if err := os.MkdirAll(parentResolved, 0o755); err != nil {
			return "", fmt.Errorf("create config workspace parent directories: %w", err)
		}
	case errors.Is(err, ErrNotFound):
		return "", ErrNotFound
	default:
		return "", err
	}

	parentInfo, err := os.Stat(parentResolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("stat config workspace parent: %w", err)
	}
	if !parentInfo.IsDir() {
		return "", ErrPathNotDirectory
	}
	parentPermissions := fsmeta.Inspect(parentResolved, parentInfo)
	if !parentPermissions.Writable {
		return "", ErrPermissionDenied
	}

	targetPath := filepath.Join(parentResolved, filepath.Base(filepath.FromSlash(normalizedPath)))
	if err := s.ensureWithinRoot(targetPath); err != nil {
		return "", err
	}

	return targetPath, nil
}

func (s *Service) resolveExistingPath(relativePath string) (string, error) {
	if err := os.MkdirAll(s.workspaceRoot, 0o755); err != nil {
		return "", fmt.Errorf("create config workspace root: %w", err)
	}

	targetPath := s.workspaceRoot
	if relativePath != "" {
		targetPath = filepath.Join(s.workspaceRoot, filepath.FromSlash(relativePath))
	}

	resolvedPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("resolve config workspace path: %w", err)
	}

	resolvedAbsolute, err := filepath.Abs(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("resolve absolute config workspace path: %w", err)
	}

	if err := s.ensureWithinRoot(resolvedAbsolute); err != nil {
		return "", err
	}

	return resolvedAbsolute, nil
}

func (s *Service) ensureWithinRoot(target string) error {
	rootAbsolute, err := filepath.Abs(s.workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve absolute workspace root: %w", err)
	}
	if resolvedRoot, err := filepath.EvalSymlinks(rootAbsolute); err == nil {
		rootAbsolute = resolvedRoot
	}
	targetAbsolute, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolve absolute target path: %w", err)
	}
	if resolvedTarget, err := filepath.EvalSymlinks(targetAbsolute); err == nil {
		targetAbsolute = resolvedTarget
	}

	relative, err := filepath.Rel(rootAbsolute, targetAbsolute)
	if err != nil {
		return fmt.Errorf("compare workspace path: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrPathOutsideWorkspace
	}

	return nil
}

func detectEntryType(path string, info os.FileInfo) (EntryType, error) {
	switch {
	case info.IsDir():
		return EntryTypeDirectory, nil
	case !info.Mode().IsRegular():
		return EntryTypeUnknownFile, nil
	case info.Size() == 0:
		return EntryTypeTextFile, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return EntryTypeUnknownFile, nil
		}
		return EntryTypeUnknownFile, fmt.Errorf("open config workspace file for inspection: %w", err)
	}
	defer file.Close()

	buffer := make([]byte, 8192)
	readBytes, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, os.ErrPermission) {
			return EntryTypeUnknownFile, nil
		}
		return EntryTypeUnknownFile, fmt.Errorf("read config workspace file for inspection: %w", err)
	}
	sample := buffer[:readBytes]

	if len(sample) == 0 {
		return EntryTypeTextFile, nil
	}
	if strings.ContainsRune(string(sample), '\x00') || !utf8.Valid(sample) {
		return EntryTypeBinaryFile, nil
	}

	return EntryTypeTextFile, nil
}

func configBlockedReason(readable, writable bool, entryType EntryType) *string {
	if !readable {
		reason := "not_readable"
		return &reason
	}
	if entryType == EntryTypeTextFile && !writable {
		reason := "not_writable"
		return &reason
	}
	return nil
}

func deriveStackID(relativePath string) *string {
	if relativePath == "" {
		return nil
	}
	firstSegment := strings.Split(relativePath, "/")[0]
	if !stacks.IsValidStackID(firstSegment) {
		return nil
	}
	stackID := firstSegment
	return &stackID
}

func normalizeRequiredFilePath(value string) (string, error) {
	normalized, err := normalizeRelativePath(value)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", ErrPathNotFile
	}
	return normalized, nil
}

func normalizeRequiredRepairPath(value string) (string, error) {
	normalized, err := normalizeRelativePath(value)
	if err != nil {
		return "", err
	}
	if normalized == "" {
		return "", ErrPathOutsideWorkspace
	}
	return normalized, nil
}

func normalizeRelativePath(value string) (string, error) {
	trimmed := strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if trimmed == "" || trimmed == "." {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "/") {
		return "", ErrPathOutsideWorkspace
	}

	parts := strings.Split(trimmed, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", ErrPathOutsideWorkspace
		default:
			cleanParts = append(cleanParts, part)
		}
	}

	return strings.Join(cleanParts, "/"), nil
}

func joinRelativePath(basePath, name string) string {
	if basePath == "" {
		return name
	}
	return basePath + "/" + name
}

func parentRelativePath(relativePath string) string {
	if relativePath == "" {
		return ""
	}
	parent := path.Dir(relativePath)
	if parent == "." {
		return ""
	}
	return parent
}

func writeFileAtomic(path, content string) error {
	return atomicfile.WriteString(path, content, ".stacklab-config-*")
}
