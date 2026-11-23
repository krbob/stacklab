package stacks

import (
	"slices"
	"sort"
	"strings"

	"stacklab/internal/store"
)

// Display the persisted failed attempt only for live replicas still using
// the previous image. Missing runtime inventory cannot resolve the record.
func activeImageUpdateWarnings(stackID string, definition definitionSnapshot, runtime map[string][]Container, saved []store.ImageUpdateWarning) []ImageUpdateWarning {
	if definition.ConfigState == ConfigStateInvalid || len(saved) == 0 {
		return nil
	}
	services := make(map[string]Service, len(definition.Services))
	for _, service := range definition.Services {
		services[service.Name] = service
	}
	var result []ImageUpdateWarning
	for _, w := range saved {
		service, exists := services[w.ServiceName]
		if !exists {
			continue
		}
		ref := ""
		if service.ImageRef != nil {
			ref = *service.ImageRef
		} else if service.Mode != ServiceModeBuild {
			continue
		}
		if ref != w.ImageRef {
			continue
		}
		project := w.ProjectName
		if project == "" {
			project = stackID
		}
		// These targets can change even while the literal Compose image string
		// stays the same. Require the reviewed file inputs for them.
		dynamicTarget := service.Mode != ServiceModeImage || strings.Contains(w.ImageRef, "$") ||
			project != stackID || definition.ProjectName != "" || strings.Contains(definition.Env, "COMPOSE_PROJECT_NAME")
		if dynamicTarget && w.DefinitionFingerprint != definition.ComposeSHA256+":"+definition.EnvSHA256 {
			continue
		}
		var names []string
		for _, container := range runtime[project] {
			if container.ServiceName == w.ServiceName && container.Status == "running" && !container.oneOff && slices.Contains(w.PreviousImageIDs, container.ImageID) && container.Name != "" {
				names = append(names, container.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		sort.Strings(names)
		result = append(result, ImageUpdateWarning{
			ServiceName: w.ServiceName, ContainerNames: names, Outcome: w.Outcome,
			AttemptedAt: w.AttemptedAt, JobID: w.JobID,
		})
	}
	return result
}
