package backup

import (
	"strings"
	"time"
)

const maxSnapshotEvidence = 4096
const snapshotClassificationVersion = 1

type snapshotClassification struct {
	Version      int       `json:"version"`
	Status       string    `json:"status"`
	ClassifiedAt time.Time `json:"classified_at"`
}

func (s *Service) snapshotClassificationLocked(destinationID, id string) (snapshotClassification, bool) {
	key := snapshotEvidenceKey(destinationID, id)
	if evidence, found := s.state.SnapshotClassifications[key]; found && evidence.Version == snapshotClassificationVersion && !evidence.ClassifiedAt.IsZero() {
		switch evidence.Status {
		case "complete", "incomplete", "legacy", "unsupported", "invalid":
			return evidence, true
		}
	}
	// Compatibility for state loaded by older integrations; normal startup
	// migrates this bounded cache and removes the old representation.
	if at, found := s.state.VerifiedSnapshots[key]; found {
		return snapshotClassification{Version: snapshotClassificationVersion, Status: "complete", ClassifiedAt: at}, true
	}
	return snapshotClassification{}, false
}

func (s *Service) rememberSnapshotClassificationLocked(destinationID, id, status string, at time.Time) {
	if s.state.SnapshotClassifications == nil {
		s.state.SnapshotClassifications = map[string]snapshotClassification{}
	}
	s.state.SnapshotClassifications[snapshotEvidenceKey(destinationID, id)] = snapshotClassification{Version: snapshotClassificationVersion, Status: status, ClassifiedAt: at}
	for len(s.state.SnapshotClassifications) > maxSnapshotEvidence {
		oldest := ""
		for key, evidence := range s.state.SnapshotClassifications {
			other := s.state.SnapshotClassifications[oldest]
			if oldest == "" || evidence.ClassifiedAt.Before(other.ClassifiedAt) || evidence.ClassifiedAt.Equal(other.ClassifiedAt) && key < oldest {
				oldest = key
			}
		}
		delete(s.state.SnapshotClassifications, oldest)
	}
}

func (s *Service) rememberRetentionCursorLocked(planID, destinationID, id string) {
	if s.state.RetentionCursors == nil {
		s.state.RetentionCursors = map[string]string{}
	}
	// Cursors refer only to configured plan/destination pairs. Removing stale
	// pairs prevents abandoned plans from growing persistent metadata forever.
	for key := range s.state.RetentionCursors {
		parts := strings.SplitN(key, "/", 2)
		if len(parts) != 2 {
			delete(s.state.RetentionCursors, key)
			continue
		}
		_, planExists := s.state.Plans[parts[0]]
		_, destinationExists := s.state.Destinations[parts[1]]
		if !planExists || !destinationExists {
			delete(s.state.RetentionCursors, key)
		}
	}
	s.state.RetentionCursors[checkpointKey(planID, destinationID)] = id
}

func snapshotEvidenceKey(destinationID, snapshotID string) string {
	return destinationID + "/" + snapshotID
}

// Snapshot IDs identify immutable authenticated objects. A bounded cache avoids
// downloading every historical manifest on every retention run. Eviction only
// causes revalidation; it can never make an unverified object eligible.
func rememberSnapshotEvidence(values *map[string]time.Time, key string, at time.Time) {
	if *values == nil {
		*values = map[string]time.Time{}
	}
	(*values)[key] = at
	for len(*values) > maxSnapshotEvidence {
		oldest := ""
		for candidate, timestamp := range *values {
			if oldest == "" || timestamp.Before((*values)[oldest]) || timestamp.Equal((*values)[oldest]) && candidate < oldest {
				oldest = candidate
			}
		}
		delete(*values, oldest)
	}
}

func (s *Service) restoreTestedAtLocked(d Destination, id string) *time.Time {
	if tested, found := s.state.RestoreTests[snapshotEvidenceKey(d.ID, id)]; found {
		return &tested
	}
	// Migration retains the final proof written by earlier versions.
	if d.LastRestoreTestSnapshotID == id {
		return d.LastRestoreTestAt
	}
	return nil
}
