package stacks

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"stacklab/internal/limitedio"
)

var validServiceHash = regexp.MustCompile(`^[a-f0-9]{64}$`)
var previousRollbackTag = regexp.MustCompile(`^stacklab\.local/maintenance-rollback:[a-f0-9]{32}-([a-f0-9]{64})$`)

// Compose v2's config --hash on the source file can omit env_file resolution.
// Feed its resolved model back to Compose so the hash reflects the actual
// container environment and the same escaped dollar literals as Compose up.
func (s *ServiceReader) rollbackConfigHashes(ctx context.Context, baseline MaintenanceHealthBaseline, resolved []byte) (map[string]string, map[string]string, error) {
	root, err := filepath.Abs(s.cfg.RootDir)
	if err != nil {
		return nil, nil, err
	}
	paths := stackPaths(root, baseline.stackID)
	hashes := func(input []byte) (map[string]string, error) {
		command, args := composeCommand(ctx, paths.RootPath, "-", paths.EnvFilePath)
		if len(baseline.serviceNames) > 0 {
			args = append(args, "--profile", "*")
		}
		cmd := exec.CommandContext(ctx, command, append(args, "config", "--hash", "*")...)
		cmd.Dir = paths.RootPath
		cmd.Stdin = bytes.NewReader(input)
		out := limitedio.NewBuffer(MaxComposeOutputBytes)
		cmd.Stdout = out
		if err := cmd.Run(); err != nil {
			return nil, err
		}
		if err := out.Err(); err != nil {
			return nil, err
		}
		result := map[string]string{}
		for line := range strings.SplitSeq(out.String(), "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && validServiceHash.MatchString(fields[1]) {
				result[fields[0]] = fields[1]
			}
		}
		return result, nil
	}
	canonical, err := hashes(resolved)
	if err != nil {
		return nil, nil, err
	}
	// A prior rollback changes only image and pull_policy. Verify both the
	// canonical hash encoded in its private tag and the hash actually deployed.
	priorImages := map[string]string{}
	for name, health := range baseline.before {
		match := previousRollbackTag.FindStringSubmatch(health.imageRef)
		if len(match) > 0 && match[1] == canonical[name] && health.configHash != canonical[name] {
			priorImages[name] = health.imageRef
		}
	}
	if len(priorImages) == 0 {
		return canonical, nil, nil
	}
	var model map[string]any
	if err := yaml.Unmarshal(resolved, &model); err != nil {
		return canonical, nil, nil
	}
	services, _ := model["services"].(map[string]any)
	for name, image := range priorImages {
		service, ok := services[name].(map[string]any)
		if !ok {
			return canonical, nil, nil
		}
		service["image"], service["pull_policy"] = image, "never"
	}
	override, err := yaml.Marshal(model)
	if err != nil {
		return canonical, nil, nil
	}
	prior, err := hashes(override)
	if err != nil {
		return canonical, nil, nil
	}
	for name := range prior {
		if _, ok := priorImages[name]; !ok {
			delete(prior, name)
		}
	}
	return canonical, prior, nil
}
