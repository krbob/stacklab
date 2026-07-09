package stacks

import "testing"

func TestLintCompose(t *testing.T) {
	content := []byte(`
services:
  good:
    image: nginx
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:80"
    healthcheck:
      test: ["CMD", "true"]
  bad:
    image: nginx
    ports:
      - "9090:80"
  short:
    image: nginx
    restart: unless-stopped
    ports:
      - "8080"
    healthcheck:
      test: ["CMD", "true"]
  numeric:
    image: nginx
    restart: unless-stopped
    ports:
      - 8081
    healthcheck:
      test: ["CMD", "true"]
`)
	warnings := LintCompose(content)
	codes := map[string]int{}
	publicPortWarnings := map[string]bool{}
	for _, warning := range warnings {
		if warning.Service == "good" {
			t.Fatalf("unexpected warning for good service: %+v", warning)
		}
		if warning.Code == "public_port_bind" {
			publicPortWarnings[warning.Service] = true
		}
		codes[warning.Code]++
	}
	if codes["missing_healthcheck"] != 1 || codes["missing_restart_policy"] != 1 || codes["public_port_bind"] != 3 {
		t.Fatalf("codes = %v", codes)
	}
	for _, service := range []string{"bad", "short", "numeric"} {
		if !publicPortWarnings[service] {
			t.Fatalf("public_port_bind warning missing for %q: %+v", service, warnings)
		}
	}
}

func TestLintComposeInvalidYAML(t *testing.T) {
	if warnings := LintCompose([]byte("services: [")); warnings != nil {
		t.Fatalf("warnings = %v, want nil", warnings)
	}
}
