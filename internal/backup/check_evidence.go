package backup

import (
	"errors"
	"strings"
	"time"
)

// These suffixes were produced by Stacklab's fixed diagnostic classifier. An
// arbitrary legacy message, or the old integrity_failed catch-all, cannot prove
// that a historical failure was only a transport problem.
func knownNonIntegrityFailure(message string) bool {
	message = strings.TrimSpace(message)
	for _, code := range []string{"unavailable", "timeout", "cancelled", "repository_locked", "repository_missing", "credentials_rejected", "permission_denied", "storage_full"} {
		if strings.HasSuffix(message, "("+code+")") {
			return true
		}
	}
	return false
}

// Older versions stored the last successful scope alongside a later failed
// check. Known non-integrity failures are attempts only, including erroneous
// failed/unknown evidence already migrated by round 3. Ambiguous failures remain
// unresolved data evidence until a complete data check succeeds.
func normalizeCheckEvidence(d *Destination) {
	legacyStatus, legacyError := d.CheckStatus, d.CheckError
	repairedNonIntegrity := false
	for _, evidence := range []*CheckEvidence{&d.MetadataCheck, &d.DataCheck} {
		if evidence.Status == "failed" && evidence.Scope == "unknown" && knownNonIntegrityFailure(evidence.Error) {
			*evidence = CheckEvidence{}
			repairedNonIntegrity = true
		}
	}
	if d.CheckStatus == "failed" && d.MetadataCheck.Status != "failed" && d.DataCheck.Status != "failed" {
		if knownNonIntegrityFailure(d.CheckError) {
			repairedNonIntegrity = true
		} else {
			d.DataCheck = CheckEvidence{Status: "failed", Scope: "unknown", CheckedAt: d.LastCheckAttemptAt, Error: d.CheckError}
		}
	}
	if repairedNonIntegrity && d.LastCheckAttemptStatus == "" && knownNonIntegrityFailure(legacyError) {
		d.LastCheckAttemptStatus, d.LastCheckAttemptError = "failed", legacyError
		// LastCheckPolicy described the last success, not this failed attempt.
		if d.LastCheckAttemptPolicy == "" {
			d.LastCheckAttemptPolicy = "unknown"
		}
	}
	if legacyStatus == "verified" || repairedNonIntegrity && d.LastCheckAt != nil {
		if d.MetadataCheck.Status == "" || d.MetadataCheck.Status == "unknown" {
			d.MetadataCheck = CheckEvidence{Status: "verified", Scope: "metadata", CheckedAt: d.LastCheckAt}
		}
		if d.DataCheck.Status == "" || d.DataCheck.Status == "unknown" {
			if d.LastCheckPolicy == "all" {
				d.DataCheck = CheckEvidence{Status: "verified", Scope: "all", CheckedAt: d.LastCheckAt}
			} else if d.LastCheckPolicy == "subset" {
				d.DataCheck = CheckEvidence{Status: "sampled", Scope: "subset", CheckedAt: d.LastCheckAt}
			}
		}
	}
	for _, evidence := range []*CheckEvidence{&d.MetadataCheck, &d.DataCheck} {
		if evidence.Status == "" {
			evidence.Status = "unknown"
		}
		if evidence.Scope == "" {
			evidence.Scope = "unknown"
		}
	}
	updateAggregateCheckEvidence(d)
}

func updateAggregateCheckEvidence(d *Destination) {
	d.CheckStatus, d.CheckError = "unknown", ""
	// Data failure remains the most consequential evidence even when metadata
	// was successfully checked more recently.
	for _, evidence := range []CheckEvidence{d.DataCheck, d.MetadataCheck} {
		if evidence.Status == "failed" {
			d.CheckStatus, d.CheckError = "failed", evidence.Error
			return
		}
	}
	if d.MetadataCheck.Status == "verified" || d.DataCheck.Status == "verified" || d.DataCheck.Status == "sampled" {
		d.CheckStatus = "verified"
	}
}

func recordCheckAttempt(d *Destination, policy string, at time.Time, err error) {
	normalizeCheckEvidence(d)
	if policy == "" {
		policy = "metadata"
	}
	d.LastCheckAttemptAt, d.LastCheckAttemptPolicy = &at, policy
	d.LastCheckAttemptStatus, d.LastCheckAttemptError = "succeeded", ""
	if err != nil {
		d.LastCheckAttemptStatus, d.LastCheckAttemptError = "failed", err.Error()
		var commandErr *repositoryCommandError
		// Connectivity, lock, cancellation and local execution failures are not
		// evidence that authenticated repository objects are corrupt.
		if errors.As(err, &commandErr) && commandErr.Code == "integrity_failed" {
			evidence := CheckEvidence{Status: "failed", Scope: policy, CheckedAt: &at, Error: err.Error()}
			if policy == "metadata" {
				d.MetadataCheck = evidence
			} else {
				d.DataCheck = evidence
			}
		}
	} else {
		d.LastCheckAt, d.LastCheckPolicy = &at, policy
		d.MetadataCheck = CheckEvidence{Status: "verified", Scope: "metadata", CheckedAt: &at}
		if policy == "all" {
			d.DataCheck = CheckEvidence{Status: "verified", Scope: "all", CheckedAt: &at}
		} else if policy == "subset" && d.DataCheck.Status != "failed" && d.DataCheck.Status != "verified" {
			d.DataCheck = CheckEvidence{Status: "sampled", Scope: "subset", CheckedAt: &at}
		}
	}
	updateAggregateCheckEvidence(d)
}
