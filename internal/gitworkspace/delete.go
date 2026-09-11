package gitworkspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"stacklab/internal/stacks"
)

var (
	ErrPathNotFile  = errors.New("git workspace target is not a regular file")
	ErrReservedPath = errors.New("stack compose and environment files cannot be deleted here")
	ErrConflict     = errors.New("git workspace file changed")
)

type DeleteFileRequest struct {
	Path               string    `json:"path"`
	ExpectedModifiedAt time.Time `json:"expected_modified_at"`
}

type DeleteFileResponse struct {
	Deleted     bool    `json:"deleted"`
	Path        string  `json:"path"`
	Scope       Scope   `json:"scope"`
	StackID     *string `json:"stack_id"`
	AuditAction string  `json:"audit_action"`
}

// deletionTarget opens each directory relative to an already-open parent.
// Symlinks are not followed, and OpenRoot confines concurrent path replacements.
func (s *Service) deletionTarget(requestedPath string) (*os.Root, string, os.FileInfo, error) {
	normalized, err := normalizeManagedPath(requestedPath)
	if err != nil {
		return nil, "", nil, err
	}
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == ".git" {
			return nil, "", nil, ErrInvalidManagedPath
		}
	}
	if parts[0] == string(ScopeStacks) {
		if len(parts) < 3 || !stacks.IsValidStackID(parts[1]) {
			return nil, "", nil, ErrInvalidManagedPath
		}
		if len(parts) == 3 && (parts[2] == "compose.yaml" || parts[2] == ".env") {
			return nil, "", nil, ErrReservedPath
		}
	}
	parent, err := os.OpenRoot(s.workspaceRoot)
	if err != nil {
		return nil, "", nil, deletionError(err)
	}
	for _, part := range parts[:len(parts)-1] {
		info, err := parent.Lstat(part)
		if err != nil {
			parent.Close()
			return nil, "", nil, deletionError(err)
		}
		if !info.IsDir() {
			parent.Close()
			return nil, "", nil, ErrPathOutsideWorkspace
		}
		next, err := parent.OpenRoot(part)
		parent.Close()
		if err != nil {
			return nil, "", nil, deletionError(err)
		}
		parent = next
	}
	name := parts[len(parts)-1]
	info, err := parent.Lstat(name)
	if err != nil {
		parent.Close()
		return nil, "", nil, deletionError(err)
	}
	if !info.Mode().IsRegular() {
		parent.Close()
		return nil, "", nil, ErrPathNotFile
	}
	return parent, name, info, nil
}

func (s *Service) DeleteFile(ctx context.Context, request DeleteFileRequest) (DeleteFileResponse, error) {
	if err := ctx.Err(); err != nil {
		return DeleteFileResponse{}, err
	}
	if request.ExpectedModifiedAt.IsZero() {
		return DeleteFileResponse{}, ErrValidation
	}
	normalized, err := normalizeManagedPath(request.Path)
	if err != nil {
		return DeleteFileResponse{}, err
	}
	if !s.mutationMu.TryLock() {
		return DeleteFileResponse{}, ErrOperationInProgress
	}
	defer s.mutationMu.Unlock()
	parent, name, info, err := s.deletionTarget(normalized)
	if err != nil {
		return DeleteFileResponse{}, err
	}
	defer parent.Close()
	if !info.ModTime().Equal(request.ExpectedModifiedAt) {
		return DeleteFileResponse{}, ErrConflict
	}
	status, err := s.Status(ctx)
	if err != nil {
		return DeleteFileResponse{}, err
	}
	if !status.Available {
		return DeleteFileResponse{}, ErrUnavailable
	}
	found := false
	for _, item := range status.Items {
		if item.Path == normalized && item.Status != FileStatusDeleted {
			found = true
			break
		}
	}
	if !found {
		return DeleteFileResponse{}, ErrNotFound
	}
	// Git inspection may take time. Reject a replaced or edited file before unlink.
	current, err := parent.Lstat(name)
	if err != nil {
		return DeleteFileResponse{}, deletionError(err)
	}
	if !os.SameFile(info, current) || !current.ModTime().Equal(request.ExpectedModifiedAt) {
		return DeleteFileResponse{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return DeleteFileResponse{}, err
	}
	if err := parent.Remove(name); err != nil {
		return DeleteFileResponse{}, deletionError(err)
	}
	scope, stackID, _, _ := managedPathContext(normalized)
	return DeleteFileResponse{Deleted: true, Path: normalized, Scope: scope, StackID: stackID, AuditAction: "delete_config_file"}, nil
}

func deletionError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, os.ErrPermission):
		return ErrPermissionDenied
	default:
		return fmt.Errorf("delete managed workspace file: %w", err)
	}
}
