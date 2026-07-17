package stacks

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Compose lint (Slice E): advisory checks for risky or missing operational
// defaults. Warnings never fail validation — they surface in the editor next
// to the resolved-config result.

type lintDefinition struct {
	Services map[string]lintService `yaml:"services"`
}

type lintService struct {
	Restart     string `yaml:"restart"`
	Healthcheck any    `yaml:"healthcheck"`
	NetworkMode string `yaml:"network_mode"`
	Ports       []any  `yaml:"ports"`
}

// LintCompose inspects raw compose content; unparsable content yields no
// warnings (validation reports the real error).
func LintCompose(content []byte) []ComposeWarning {
	var doc lintDefinition
	if err := yaml.Unmarshal(content, &doc); err != nil || len(doc.Services) == 0 {
		return nil
	}

	names := make([]string, 0, len(doc.Services))
	for name := range doc.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	warnings := []ComposeWarning{}
	for _, name := range names {
		service := doc.Services[name]

		if service.Healthcheck == nil {
			warnings = append(warnings, ComposeWarning{
				Code:    "missing_healthcheck",
				Service: name,
				Message: fmt.Sprintf("Service %q does not declare a healthcheck in Compose. It may inherit one from its image; otherwise hangs and readiness failures cannot be detected.", name),
			})
		}
		if service.Restart == "" || service.Restart == "no" {
			warnings = append(warnings, ComposeWarning{
				Code:    "missing_restart_policy",
				Service: name,
				Message: fmt.Sprintf("Service %q has no restart policy; it will stay down after a crash or reboot.", name),
			})
		}
		for _, port := range service.Ports {
			if publicPortBind(port) {
				warnings = append(warnings, ComposeWarning{
					Code:    "public_port_bind",
					Service: name,
					Message: fmt.Sprintf("Service %q publishes a port on all interfaces; bind to a specific address if it should stay internal.", name),
				})
				break
			}
		}
	}
	return warnings
}

// publicPortBind reports whether a port entry publishes on all interfaces —
// short syntax without a host IP, an explicit 0.0.0.0, or long syntax
// without host_ip.
func publicPortBind(entry any) bool {
	switch value := entry.(type) {
	case string:
		if !strings.Contains(value, ":") {
			return true // container port published on a random host port
		}
		parts := strings.Split(value, ":")
		if len(parts) == 2 {
			return true // host:container with implicit 0.0.0.0
		}
		host := strings.Trim(strings.Join(parts[:len(parts)-2], ":"), "[]")
		return host == "0.0.0.0" || host == "::"
	case int:
		return true
	case map[string]any:
		if _, published := value["published"]; !published {
			return false
		}
		hostIP, _ := value["host_ip"].(string)
		return hostIP == "" || hostIP == "0.0.0.0" || hostIP == "::"
	default:
		return false
	}
}
