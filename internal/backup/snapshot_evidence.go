package backup

import "time"

const maxSnapshotEvidence = 4096

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
