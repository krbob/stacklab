package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	if legacyStatus == "failed" && d.LastCheckAttemptStatus == "" {
		d.LastCheckAttemptStatus, d.LastCheckAttemptError = "failed", legacyError
		// LastCheckPolicy described the last success, not this failed attempt.
		if d.LastCheckAttemptPolicy == "" {
			d.LastCheckAttemptPolicy = "unknown"
		}
	}
	// Only this fixed historical diagnostic identifies a completed but
	// inconclusive repository check. Generic operation failures and transport
	// errors must not be reinterpreted as a repository hold.
	if d.CheckHold == nil && d.LastCheckAttemptStatus == "failed" &&
		strings.Contains(d.LastCheckAttemptError, "repository check was inconclusive") &&
		strings.HasSuffix(d.LastCheckAttemptError, "(operation_failed)") &&
		!laterCheckResolves(d.LastCheckAt, d.LastCheckPolicy, d.LastCheckAttemptAt, requiredCheckScope(d.LastCheckAttemptPolicy)) {
		d.CheckHold = &CheckHold{Scope: requiredCheckScope(d.LastCheckAttemptPolicy), CheckedAt: d.LastCheckAttemptAt, Error: d.LastCheckAttemptError}
	}
	if d.CheckHold != nil {
		hold := *d.CheckHold
		hold.Scope = requiredCheckScope(hold.Scope)
		d.CheckHold = &hold
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
	defer func() { d.MaintenanceReviewToken = repositoryReviewFingerprint(*d) }()
	d.CheckStatus, d.CheckError = "unknown", ""
	// Data failure remains the most consequential evidence even when metadata
	// was successfully checked more recently.
	for _, evidence := range []CheckEvidence{d.DataCheck, d.MetadataCheck} {
		if evidence.Status == "failed" {
			d.CheckStatus, d.CheckError = "failed", evidence.Error
			return
		}
	}
	if d.CheckHold != nil {
		d.CheckStatus, d.CheckError = "unverified", d.CheckHold.Error
		return
	}
	if d.MetadataCheck.Status == "verified" || d.DataCheck.Status == "verified" || d.DataCheck.Status == "sampled" {
		d.CheckStatus = "verified"
	}
}

func requiredCheckScope(policy string) string {
	if policy == "metadata" {
		return "metadata"
	}
	return "all"
}

func laterCheckResolves(successAt *time.Time, successPolicy string, failedAt *time.Time, requiredScope string) bool {
	if successAt == nil || failedAt == nil || !successAt.After(*failedAt) {
		return false
	}
	return successPolicy == "all" || requiredScope == "metadata" && (successPolicy == "metadata" || successPolicy == "subset")
}

func repositoryMaintenanceBlocked(d Destination) bool {
	return d.CheckHold != nil || d.MetadataCheck.Status == "failed" || d.DataCheck.Status == "failed" || d.CheckStatus == "failed"
}

// Review only unresolved evidence, not the most recent operation or transient
// attempt. This keeps acknowledgement stable across transport failures while
// invalidating it when the repository's unresolved condition changes.
func repositoryReviewFingerprint(d Destination) string {
	if !repositoryMaintenanceBlocked(d) {
		return ""
	}
	type condition struct {
		Scope     string `json:"scope"`
		CheckedAt string `json:"checked_at,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	makeCondition := func(scope string, at *time.Time, message string) *condition {
		value := &condition{Scope: scope, Error: message}
		if at != nil {
			value.CheckedAt = at.UTC().Format(time.RFC3339Nano)
		}
		return value
	}
	review := struct {
		Version     int        `json:"version"`
		Destination string     `json:"destination"`
		Metadata    *condition `json:"metadata,omitempty"`
		Data        *condition `json:"data,omitempty"`
		Hold        *condition `json:"hold,omitempty"`
	}{Version: 1, Destination: d.ID}
	if d.MetadataCheck.Status == "failed" {
		review.Metadata = makeCondition(d.MetadataCheck.Scope, d.MetadataCheck.CheckedAt, d.MetadataCheck.Error)
	}
	if d.DataCheck.Status == "failed" {
		review.Data = makeCondition(d.DataCheck.Scope, d.DataCheck.CheckedAt, d.DataCheck.Error)
	}
	if d.CheckHold != nil {
		review.Hold = makeCondition(d.CheckHold.Scope, d.CheckHold.CheckedAt, d.CheckHold.Error)
	}
	if review.Metadata == nil && review.Data == nil && review.Hold == nil {
		// Also cover callers inspecting an unnormalised legacy destination.
		review.Data = makeCondition("unknown", d.LastCheckAttemptAt, d.CheckError)
	}
	encoded, _ := json.Marshal(review)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
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
		} else if commandErr != nil && commandErr.Code == "check_inconclusive" {
			hold := &CheckHold{Scope: requiredCheckScope(policy), CheckedAt: &at, Error: err.Error()}
			// A metadata-only attempt cannot replace a still unresolved full
			// read with a condition that a metadata check could clear.
			if d.CheckHold == nil || d.CheckHold.Scope == "metadata" || hold.Scope == "all" {
				d.CheckHold = hold
			}
		}
	} else {
		if d.CheckHold != nil && (policy == "all" || d.CheckHold.Scope == "metadata") {
			d.CheckHold = nil
		}
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
