package stacks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var templateVariableNameRegexp = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// Stack templates (dashboard read-model contract, Slice F): curated local
// starters under <root>/templates/<id>/ (template.yaml + compose.yaml).
// When the operator has none, a small built-in set keeps the create flow
// useful out of the box. Compose-first and fully transparent: a template is
// just a compose.yaml the editor starts from.

type TemplatesResponse struct {
	Items []StackTemplate `json:"items"`
}

type StackTemplate struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Icon        string             `json:"icon,omitempty"`
	ComposeYAML string             `json:"compose_yaml"`
	BuiltIn     bool               `json:"built_in"`
	Variables   []TemplateVariable `json:"variables,omitempty"`
}

type TemplateVariable struct {
	Name        string `json:"name"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
	Default     string `json:"default,omitempty"`
	Required    bool   `json:"required"`
}

type templateManifest struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	Icon        string             `yaml:"icon"`
	Variables   []TemplateVariable `yaml:"variables"`
}

var builtInTemplates = []StackTemplate{
	{
		ID:          "web-service",
		Name:        "Generic web service",
		Description: "Single HTTP container with a published port, restart policy, and Stacklab link metadata.",
		Icon:        "globe",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "IMAGE", Label: "Image", Description: "Container image to run.", Default: "nginx:stable-alpine", Required: true},
			{Name: "HOST_PORT", Label: "Host port", Description: "Port exposed on the Docker host.", Default: "8080", Required: true},
			{Name: "CONTAINER_PORT", Label: "Container port", Description: "Port exposed by the container.", Default: "80", Required: true},
			{Name: "WEB_URL", Label: "Web URL", Description: "Link shown on the stack card.", Default: "http://localhost:8080", Required: false},
		},
		ComposeYAML: `services:
  app:
    image: ${IMAGE}
    restart: unless-stopped
    ports:
      - "${HOST_PORT}:${CONTAINER_PORT}"

x-stacklab:
  icon: globe
  links:
    - label: Web UI
      url: ${WEB_URL}
`,
	},
	{
		ID:          "static-site",
		Name:        "Static site",
		Description: "Nginx serving files from a stack-local ./site directory.",
		Icon:        "file-code",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "HOST_PORT", Label: "Host port", Description: "Port exposed on the Docker host.", Default: "8080", Required: true},
			{Name: "WEB_URL", Label: "Web URL", Description: "Link shown on the stack card.", Default: "http://localhost:8080", Required: false},
		},
		ComposeYAML: `services:
  web:
    image: nginx:stable-alpine
    restart: unless-stopped
    ports:
      - "${HOST_PORT}:80"
    volumes:
      - ./site:/usr/share/nginx/html:ro

x-stacklab:
  icon: file-code
  links:
    - label: Web UI
      url: ${WEB_URL}
`,
	},
	{
		ID:          "postgres-service",
		Name:        "PostgreSQL",
		Description: "Standalone PostgreSQL service with a named data volume and healthcheck.",
		Icon:        "database",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "POSTGRES_USER", Label: "Database user", Default: "app", Required: true},
			{Name: "POSTGRES_PASSWORD", Label: "Database password", Default: "change-me", Required: true},
			{Name: "POSTGRES_DB", Label: "Database name", Default: "app", Required: true},
		},
		ComposeYAML: `services:
  db:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    volumes:
      - db-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  db-data:

x-stacklab:
  icon: database
`,
	},
	{
		ID:          "app-with-db",
		Name:        "Adminer + PostgreSQL",
		Description: "Adminer web UI connected to a PostgreSQL sidecar with a named volume.",
		Icon:        "database",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "HOST_PORT", Label: "Host port", Description: "Port exposed on the Docker host.", Default: "8080", Required: true},
			{Name: "POSTGRES_USER", Label: "Database user", Default: "app", Required: true},
			{Name: "POSTGRES_PASSWORD", Label: "Database password", Default: "change-me", Required: true},
			{Name: "POSTGRES_DB", Label: "Database name", Default: "app", Required: true},
			{Name: "WEB_URL", Label: "Web URL", Description: "Link shown on the stack card.", Default: "http://localhost:8080", Required: false},
		},
		ComposeYAML: `services:
  web:
    image: adminer:latest
    restart: unless-stopped
    ports:
      - "${HOST_PORT}:8080"
    depends_on:
      db:
        condition: service_healthy
    environment:
      ADMINER_DEFAULT_SERVER: db

  db:
    image: postgres:17-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: ${POSTGRES_USER}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: ${POSTGRES_DB}
    volumes:
      - db-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER} -d ${POSTGRES_DB}"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  db-data:

x-stacklab:
  icon: database
  links:
    - label: Web UI
      url: ${WEB_URL}
`,
	},
	{
		ID:          "worker-with-redis",
		Name:        "Worker + Redis",
		Description: "Background worker pattern with a Redis sidecar and healthcheck.",
		Icon:        "activity",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "WORKER_INTERVAL", Label: "Worker interval", Description: "Seconds between worker checks.", Default: "30", Required: true},
		},
		ComposeYAML: `services:
  worker:
    image: redis:7-alpine
    restart: unless-stopped
    command: ["sh", "-c", "while true; do redis-cli -h redis ping; sleep ${WORKER_INTERVAL}; done"]
    depends_on:
      redis:
        condition: service_healthy

  redis:
    image: redis:7-alpine
    restart: unless-stopped
    volumes:
      - redis-data:/data
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 3s
      retries: 5

volumes:
  redis-data:

x-stacklab:
  icon: activity
`,
	},
	{
		ID:          "volume-backed-service",
		Name:        "File browser",
		Description: "File Browser with a named data volume for simple persistent storage.",
		Icon:        "folder",
		BuiltIn:     true,
		Variables: []TemplateVariable{
			{Name: "HOST_PORT", Label: "Host port", Description: "Port exposed on the Docker host.", Default: "8080", Required: true},
			{Name: "WEB_URL", Label: "Web URL", Description: "Link shown on the stack card.", Default: "http://localhost:8080", Required: false},
		},
		ComposeYAML: `services:
  files:
    image: filebrowser/filebrowser:v2
    restart: unless-stopped
    ports:
      - "${HOST_PORT}:80"
    volumes:
      - files-data:/srv
      - filebrowser-db:/database

volumes:
  files-data:
  filebrowser-db:

x-stacklab:
  icon: folder
  links:
    - label: Web UI
      url: ${WEB_URL}
`,
	},
}

// Templates lists operator templates from <root>/templates, falling back to
// the built-in starters when the directory is absent or empty.
func (s *ServiceReader) Templates(_ context.Context) (TemplatesResponse, error) {
	templatesRoot := filepath.Join(s.cfg.RootDir, "templates")
	entries, err := os.ReadDir(templatesRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return TemplatesResponse{Items: builtInTemplates}, nil
		}
		return TemplatesResponse{}, err
	}

	items := make([]StackTemplate, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !IsValidStackID(entry.Name()) {
			continue
		}
		composePath := filepath.Join(templatesRoot, entry.Name(), "compose.yaml")
		composeContent, err := os.ReadFile(composePath)
		if err != nil {
			continue
		}

		template := StackTemplate{
			ID:          entry.Name(),
			Name:        entry.Name(),
			ComposeYAML: string(composeContent),
		}
		if manifestContent, err := os.ReadFile(filepath.Join(templatesRoot, entry.Name(), "template.yaml")); err == nil {
			var manifest templateManifest
			if yaml.Unmarshal(manifestContent, &manifest) == nil {
				if name := strings.TrimSpace(manifest.Name); name != "" {
					template.Name = name
				}
				template.Description = strings.TrimSpace(manifest.Description)
				template.Icon = strings.TrimSpace(manifest.Icon)
				template.Variables = normalizeTemplateVariables(manifest.Variables)
			}
		}
		items = append(items, template)
	}

	if len(items) == 0 {
		return TemplatesResponse{Items: builtInTemplates}, nil
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return TemplatesResponse{Items: items}, nil
}

func (s *ServiceReader) RenderTemplate(ctx context.Context, templateID string, variables map[string]string) (string, error) {
	templateID = strings.TrimSpace(templateID)
	if !IsValidStackID(templateID) {
		return "", ErrNotFound
	}
	response, err := s.Templates(ctx)
	if err != nil {
		return "", err
	}
	for _, template := range response.Items {
		if template.ID != templateID {
			continue
		}
		values := map[string]string{}
		for _, variable := range template.Variables {
			value, ok := variables[variable.Name]
			if !ok {
				value = variable.Default
			}
			value = strings.TrimSpace(value)
			if variable.Required && value == "" {
				return "", fmt.Errorf("%w: template variable %s is required", ErrInvalidState, variable.Name)
			}
			values[variable.Name] = value
		}
		for key, value := range variables {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			values[key] = strings.TrimSpace(value)
		}
		return renderTemplateContent(template.ComposeYAML, values)
	}
	return "", ErrNotFound
}

func renderTemplateContent(content string, values map[string]string) (string, error) {
	var missing []string
	rendered := regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)\}`).ReplaceAllStringFunc(content, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		value, ok := values[name]
		if !ok {
			missing = append(missing, name)
			return match
		}
		return value
	})
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", fmt.Errorf("%w: missing template variables: %s", ErrInvalidState, strings.Join(missing, ", "))
	}
	return rendered, nil
}

func normalizeTemplateVariables(variables []TemplateVariable) []TemplateVariable {
	result := make([]TemplateVariable, 0, len(variables))
	seen := map[string]struct{}{}
	for _, variable := range variables {
		name := strings.TrimSpace(variable.Name)
		if !templateVariableNameRegexp.MatchString(name) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		variable.Name = name
		variable.Label = strings.TrimSpace(variable.Label)
		variable.Description = strings.TrimSpace(variable.Description)
		variable.Default = strings.TrimSpace(variable.Default)
		result = append(result, variable)
	}
	return result
}
