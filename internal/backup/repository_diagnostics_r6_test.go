package backup

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// Synthetic combinations model independently emitted restic records. Native
// fixture transcripts are kept separately with their backend/version provenance.
func TestRepositoryDiagnosticRecordContext(t *testing.T) {
	pack := strings.Repeat("a", 64)
	for _, tc := range []struct{ name, diagnostic, expected string }{
		{"SFTP inventory outage", "List(data) failed: ssh command exited: exit status 255\npack " + pack + ": does not exist\nerror: failed to load snapshot aaaaaaaa: circuit breaker open for file <snapshot/aaaaaaaaaa>\ndownload error: circuit breaker open for file <data/aaaaaaaaaa>\nFatal: repository contains errors", "unavailable"},
		{"unknown tree with trailing JSON newline", "error for tree aaaaaaaa:\n  unsupported repository structure\ndownload error: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"lock refresh cannot invalidate pack inventory", "unable to refresh lock: connection reset by peer\npack " + pack + ": does not exist\nFatal: repository contains errors", "integrity_failed"},
		{"unknown blob with independent transport", "blob " + pack + " has unexpected length\ndownload error: EOF\nFatal: repository contains errors", "check_inconclusive"},
		{"unknown listing with independent transport", "List(data) failed: unexpected backend response\ndownload error: EOF\npack " + pack + ": does not exist\nFatal: repository contains errors", "check_inconclusive"},
		{"failed index does not invalidate pack listing", "Load(<index/aaaaaaaaaa>, 0, 0) failed: EOF\npack " + pack + ": does not exist", "integrity_failed"},
		{"failed pack listing does not invalidate index", "List(data) failed: EOF\nerror for tree aaaaaaaa:\n  id " + pack + " not found in repository", "integrity_failed"},
		{"explained tree transport", "error for tree aaaaaaaa:\n  download error: EOF\nThe repository is damaged and must be repaired.\nFatal: repository contains errors", "unavailable"},
		{"explained pack transport", "pack " + pack + " contains 1 errors: [partial download error: EOF]\nThe repository contains damaged pack files.\nFatal: repository contains errors", "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, format := range []string{"plain", "JSON records", "JSON multiline"} {
				t.Run(format, func(t *testing.T) {
					diagnostic := tc.diagnostic
					if format != "plain" {
						messages := []string{diagnostic}
						if format == "JSON records" {
							messages = strings.Split(diagnostic, "\n")
						}
						var records []string
						for _, line := range messages {
							encoded, _ := json.Marshal(map[string]string{"message_type": "error", "message": line + "\n"})
							records = append(records, string(encoded))
						}
						diagnostic = strings.Join(records, "\n")
					}
					assertDiagnosticCode(t, diagnostic, tc.expected)
					at := time.Now().UTC()
					d := Destination{MetadataCheck: CheckEvidence{Status: "verified", Scope: "metadata", CheckedAt: &at}, DataCheck: CheckEvidence{Status: "verified", Scope: "all", CheckedAt: &at}}
					recordCheckAttempt(&d, "all", at.Add(time.Minute), classifyCommandError("restic", errors.New("exit"), diagnostic))
					if (d.CheckHold != nil) != (tc.expected == "check_inconclusive") || repositoryMaintenanceBlocked(d) != (tc.expected != "unavailable") {
						t.Fatalf("wrong hold or maintenance decision: %+v", d)
					}
				})
			}
		})
	}
}
