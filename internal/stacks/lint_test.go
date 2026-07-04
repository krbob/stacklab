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
`)
	warnings := LintCompose(content)
	codes := map[string]int{}
	for _, warning := range warnings {
		if warning.Service == "good" {
			t.Fatalf("unexpected warning for good service: %+v", warning)
		}
		codes[warning.Code]++
	}
	if codes["missing_healthcheck"] != 1 || codes["missing_restart_policy"] != 1 || codes["public_port_bind"] != 1 {
		t.Fatalf("codes = %v", codes)
	}
}

func TestLintComposeInvalidYAML(t *testing.T) {
	if warnings := LintCompose([]byte("services: [")); warnings != nil {
		t.Fatalf("warnings = %v, want nil", warnings)
	}
}
