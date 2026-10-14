package backup

import (
	"errors"
	"time"
)

// Older versions stored the last successful scope alongside a later failed
// check. It cannot identify the scope of that failure. Preserve it as unresolved
// data evidence until a complete data check succeeds.
func normalizeCheckEvidence(d *Destination) {
	if d.CheckStatus == "failed" && d.MetadataCheck.Status != "failed" && d.DataCheck.Status != "failed" {
		d.DataCheck = CheckEvidence{Status: "failed", Scope: "unknown", CheckedAt: d.LastCheckAttemptAt, Error: d.CheckError}
	}
	if d.MetadataCheck.Status == "" && d.DataCheck.Status == "" {
		switch d.CheckStatus {
		case "verified":
			d.MetadataCheck = CheckEvidence{Status: "verified", Scope: "metadata", CheckedAt: d.LastCheckAt}
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
