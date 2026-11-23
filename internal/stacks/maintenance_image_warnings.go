package stacks

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"time"

	"stacklab/internal/limitedio"
	"stacklab/internal/store"
)

// RecordMaintenanceImageFailure runs only for a failed targeted update or a verified
// rollback. Registry manifest digests and Docker image/config IDs are separate identities.
func (s *ServiceReader) RecordMaintenanceImageFailure(ctx context.Context, job store.Job, baseline MaintenanceHealthBaseline, recovery MaintenanceImageRecovery, pullPending bool) error {
	if s.store == nil || len(baseline.targets) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resolved, err := s.maintenanceHealthConfig(ctx, baseline.stackID, baseline.serviceNames)
	if err != nil {
		return err
	}
	if contentSHA256(string(resolved)) != baseline.configDigest {
		// The operator is now targeting a different deployment.
		return nil
	}
	paths := stackPaths(s.cfg.RootDir, baseline.stackID)
	raw, err := limitedio.ReadFile(paths.ComposeFilePath, MaxDefinitionFileBytes)
	if err != nil {
		return err
	}
	services, _, err := parseComposeServices(paths.RootPath, raw)
	if err != nil {
		return errors.New("could not read service image references from Compose")
	}
	composeHash, envHash, ok := s.currentDefinitionHashes(paths.ComposeFilePath, paths.EnvFilePath)
	if !ok || composeHash != contentSHA256(string(raw)) {
		return errors.New("stack definition changed while checking image update evidence")
	}
	runtime, err := inspectMaintenanceTargets(ctx, baseline.project, baseline.targets)
	if err != nil {
		return err
	}
	available := map[string]store.ImageUpdateStatus{}
	if pullPending {
		statuses, err := s.store.ListImageUpdateStatus(ctx)
		if err != nil {
			return err
		}
		for _, status := range statuses {
			if status.State == "available" {
				available[status.ImageRef] = status
			}
		}
	}
	cached := map[string]maintenanceImageIdentity{}
	inspect := func(ref string) maintenanceImageIdentity {
		if image, ok := cached[ref]; ok {
			return image
		}
		image := inspectMaintenanceImage(ctx, ref)
		cached[ref] = image
		return image
	}
	attemptedAt := time.Now().UTC()
	var warnings []store.ImageUpdateWarning
	for _, service := range services {
		target, targeted := baseline.targets[service.Name]
		if !targeted || target.completionExpected || target.replicas == 0 {
			continue
		}
		var previous []string
		for _, c := range runtime[service.Name] {
			if c.State.Status == "running" && rollbackImageID.MatchString(c.Image) && slices.Contains(baseline.before[service.Name].runningImageIDs, c.Image) {
				previous = append(previous, c.Image)
			}
		}
		if len(previous) == 0 {
			continue
		}
		slices.Sort(previous)
		previous = slices.Compact(previous)
		desiredRef := target.image
		if desiredRef == "" && service.Mode == ServiceModeBuild {
			desiredRef = baseline.project + "-" + service.Name
		}
		if desiredRef == "" {
			continue
		}
		lagging, candidate, remote, outcome := failedImageEvidence(previous, desiredRef, recovery.restored[service.Name], available[desiredRef], inspect)
		if len(lagging) == 0 {
			continue
		}
		w := store.ImageUpdateWarning{
			StackID: baseline.stackID, ProjectName: baseline.project, ServiceName: service.Name,
			DefinitionFingerprint: composeHash + ":" + envHash, PreviousImageIDs: lagging,
			CandidateImageID: candidate, CandidateRemoteDigest: remote, Outcome: outcome,
			JobID: job.ID, AttemptedAt: attemptedAt,
		}
		if service.ImageRef != nil {
			w.ImageRef = *service.ImageRef
		}
		warnings = append(warnings, w)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.store.UpdateImageWarnings(ctx, baseline.stackID, warnings, nil)
}

// Recovery proves the exact local deployment that was attempted. For a
// requested pull that never finished, a known registry candidate takes
// precedence over the stale local tag, which may even point backwards.
func failedImageEvidence(previous []string, desiredRef, restoredFrom string, available store.ImageUpdateStatus, inspect func(string) maintenanceImageIdentity) (lagging []string, candidate, remote, outcome string) {
	outcome = "not_applied"
	switch {
	case rollbackImageID.MatchString(restoredFrom) && !slices.Contains(previous, restoredFrom):
		outcome = "rolled_back"
		candidate = restoredFrom
	case available.State == "available":
		for _, id := range previous {
			if proof := pendingMaintenanceManifest(available, inspect(id)); proof != "" {
				remote = proof
				lagging = append(lagging, id)
			}
		}
		return lagging, "", remote, outcome
	default:
		candidate = inspect(desiredRef).id
	}
	if candidate != "" {
		for _, id := range previous {
			if id != candidate {
				lagging = append(lagging, id)
			}
		}
	}
	return lagging, candidate, "", outcome
}

// ClearMaintenanceImageWarnings uses the same stabilized observations that passed
// verification. A no-pull retry that kept the previous image does not clear its warning.
func (s *ServiceReader) ClearMaintenanceImageWarnings(ctx context.Context, stackID string, report MaintenanceHealthReport) error {
	if s.store == nil || len(report.verified) == 0 {
		return nil
	}
	warnings, err := s.store.ListImageWarnings(ctx)
	if err != nil {
		return err
	}
	var clear []string
	for _, w := range warnings[stackID] {
		health, ok := report.verified[w.ServiceName]
		if !ok || !health.ready || health.instances == 0 || health.instances != health.containerCount || len(health.runningImageIDs) == 0 {
			continue
		}
		stillUnresolved := slices.ContainsFunc(health.runningImageIDs, func(id string) bool {
			return !rollbackImageID.MatchString(id) || slices.Contains(w.PreviousImageIDs, id)
		})
		if !stillUnresolved {
			clear = append(clear, w.ServiceName)
		}
	}
	return s.store.UpdateImageWarnings(ctx, stackID, nil, clear)
}

type maintenanceImageIdentity struct {
	id          string
	repoDigests []string
}

func inspectMaintenanceImage(ctx context.Context, ref string) maintenanceImageIdentity {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}} {{json .RepoDigests}}", "--", ref)
	output := limitedio.NewBuffer(128 << 10)
	cmd.Stdout = output
	if err := cmd.Run(); err != nil || output.Err() != nil {
		return maintenanceImageIdentity{}
	}
	id, digests, ok := strings.Cut(strings.TrimSpace(output.String()), " ")
	if !ok || !rollbackImageID.MatchString(id) {
		return maintenanceImageIdentity{}
	}
	image := maintenanceImageIdentity{id: id}
	if json.Unmarshal([]byte(digests), &image.repoDigests) != nil {
		image.repoDigests = nil
	}
	return image
}

// A registry check applies to the old running image only if it actually owns
// the local manifest used for that check and does not also own the remote one.
func pendingMaintenanceManifest(status store.ImageUpdateStatus, previous maintenanceImageIdentity) string {
	if !rollbackImageID.MatchString(status.RemoteDigest) || !rollbackImageID.MatchString(status.LocalDigest) {
		return ""
	}
	localMatches := false
	for _, ref := range previous.repoDigests {
		_, digest, ok := strings.Cut(ref, "@")
		if !ok {
			continue
		}
		if digest == status.RemoteDigest {
			return ""
		}
		localMatches = localMatches || digest == status.LocalDigest
	}
	if localMatches {
		return status.RemoteDigest
	}
	return ""
}
