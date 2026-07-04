package stacks

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Stack templates (dashboard read-model contract, Slice F): curated local
// starters under <root>/templates/<id>/ (template.yaml + compose.yaml).
// When the operator has none, a small built-in set keeps the create flow
// useful out of the box. Compose-first and fully transparent: a template is
// just a compose.yaml the editor starts from.

type TemplatesResponse struct {
	Items []StackTemplate `json:"items"`
}

type StackTemplate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ComposeYAML string `json:"compose_yaml"`
	BuiltIn     bool   `json:"built_in"`
}

type templateManifest struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

var builtInTemplates = []StackTemplate{
	{
		ID:          "web-service",
		Name:        "Web service",
		Description: "Single container behind the reverse proxy, with healthcheck and restart policy.",
		BuiltIn:     true,
		ComposeYAML: `services:
  app:
    image: nginx:stable
    restart: unless-stopped
    ports:
      - "8080:80"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost/"]
      interval: 30s
      timeout: 5s
      retries: 3

x-stacklab:
  links:
    - label: Web UI
      url: http://localhost:8080
`,
	},
	{
		ID:          "app-with-db",
		Name:        "App + PostgreSQL",
		Description: "Application container with a PostgreSQL sidecar and named volume.",
		BuiltIn:     true,
		ComposeYAML: `services:
  app:
    image: ghcr.io/example/app:latest
    restart: unless-stopped
    depends_on:
      db:
        condition: service_healthy
    environment:
      - DATABASE_URL=postgres://app:app@db:5432/app

  db:
    image: postgres:17
    restart: unless-stopped
    environment:
      - POSTGRES_USER=app
      - POSTGRES_PASSWORD=app
      - POSTGRES_DB=app
    volumes:
      - db-data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U app"]
      interval: 10s
      timeout: 5s
      retries: 5

volumes:
  db-data:
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
