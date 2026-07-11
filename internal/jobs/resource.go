package jobs

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrResourceConflict  = errors.New("job resource conflict")
	ErrResourcesRequired = errors.New("job resources are required")
	ErrInvalidResource   = errors.New("invalid job resource")
)

type ResourceKind uint8

const (
	ResourceKindGlobal ResourceKind = iota + 1
	ResourceKindDockerDaemon
	ResourceKindDockerRegistry
	ResourceKindSelfUpdate
	ResourceKindImageUpdates
	ResourceKindStack
)

type Resource struct {
	kind ResourceKind
	id   string
}

func GlobalResource() Resource {
	return Resource{kind: ResourceKindGlobal}
}

func DockerDaemonResource() Resource {
	return Resource{kind: ResourceKindDockerDaemon}
}

func DockerRegistryResource() Resource {
	return Resource{kind: ResourceKindDockerRegistry}
}

func SelfUpdateResource() Resource {
	return Resource{kind: ResourceKindSelfUpdate}
}

func ImageUpdatesResource() Resource {
	return Resource{kind: ResourceKindImageUpdates}
}

func StackResource(stackID string) Resource {
	return Resource{kind: ResourceKindStack, id: strings.TrimSpace(stackID)}
}

func StackResources(stackIDs []string) []Resource {
	resources := make([]Resource, 0, len(stackIDs))
	for _, stackID := range stackIDs {
		if resource := StackResource(stackID); resource.id != "" {
			resources = append(resources, resource)
		}
	}
	return resources
}

func (r Resource) Kind() ResourceKind {
	return r.kind
}

func (r Resource) StackID() string {
	if r.kind != ResourceKindStack {
		return ""
	}
	return r.id
}

func (r Resource) String() string {
	switch r.kind {
	case ResourceKindGlobal:
		return "global"
	case ResourceKindDockerDaemon:
		return "docker-daemon"
	case ResourceKindDockerRegistry:
		return "docker-registry"
	case ResourceKindSelfUpdate:
		return "self-update"
	case ResourceKindImageUpdates:
		return "image-updates"
	case ResourceKindStack:
		return "stack:" + r.id
	default:
		return "invalid"
	}
}

func (r Resource) validate() error {
	switch r.kind {
	case ResourceKindGlobal, ResourceKindDockerDaemon, ResourceKindDockerRegistry, ResourceKindSelfUpdate, ResourceKindImageUpdates:
		if r.id != "" {
			return fmt.Errorf("%w: %s resource cannot have an id", ErrInvalidResource, r.String())
		}
		return nil
	case ResourceKindStack:
		if r.id == "" {
			return fmt.Errorf("%w: stack resource requires an id", ErrInvalidResource)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown resource kind %d", ErrInvalidResource, r.kind)
	}
}

// ResourcesConflict is the single conflict matrix for mutating jobs:
//   - global excludes every other resource;
//   - Docker daemon changes exclude stack and registry mutations;
//   - Docker registry changes exclude stack mutations;
//   - otherwise only the same typed resource conflicts.
//
// Self-update exclusivity is intentionally handled by an explicit all-resource
// drain lifecycle rather than being hidden in this pairwise matrix.
func ResourcesConflict(left, right Resource) bool {
	if left == right {
		return true
	}
	if left.kind == ResourceKindGlobal || right.kind == ResourceKindGlobal {
		return true
	}
	if left.kind == ResourceKindDockerDaemon || right.kind == ResourceKindDockerDaemon {
		other := right.kind
		if left.kind != ResourceKindDockerDaemon {
			other = left.kind
		}
		return other == ResourceKindStack || other == ResourceKindDockerRegistry
	}
	if left.kind == ResourceKindDockerRegistry || right.kind == ResourceKindDockerRegistry {
		other := right.kind
		if left.kind != ResourceKindDockerRegistry {
			other = left.kind
		}
		return other == ResourceKindStack
	}
	return false
}

type ConflictReason string

const (
	ConflictReasonResourceHeld ConflictReason = "resource_held"
	ConflictReasonDrainActive  ConflictReason = "drain_active"
	ConflictReasonDrainBlocked ConflictReason = "drain_blocked"
)

type ResourceConflictError struct {
	Reason           ConflictReason
	Requested        Resource
	Conflicting      Resource
	ConflictingJobID string
}

func (e *ResourceConflictError) Error() string {
	if e == nil {
		return ErrResourceConflict.Error()
	}
	return fmt.Sprintf(
		"%s (%s): requested %s conflicts with %s held by job %s",
		ErrResourceConflict,
		e.Reason,
		e.Requested,
		e.Conflicting,
		e.ConflictingJobID,
	)
}

func (e *ResourceConflictError) Unwrap() error {
	return ErrResourceConflict
}

func normalizeResources(stackID string, requested []Resource) ([]Resource, error) {
	unique := make(map[Resource]struct{}, len(requested)+1)
	if strings.TrimSpace(stackID) != "" {
		unique[StackResource(stackID)] = struct{}{}
	}
	for _, resource := range requested {
		if err := resource.validate(); err != nil {
			return nil, err
		}
		unique[resource] = struct{}{}
	}
	if len(unique) == 0 {
		return nil, ErrResourcesRequired
	}
	if _, globallyLocked := unique[GlobalResource()]; globallyLocked {
		return []Resource{GlobalResource()}, nil
	}

	resources := make([]Resource, 0, len(unique))
	for resource := range unique {
		resources = append(resources, resource)
	}
	sort.Slice(resources, func(i, j int) bool {
		return resources[i].String() < resources[j].String()
	})
	return resources, nil
}
