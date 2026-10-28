package backup

import "bytes"

// Retain interpretation rather than the first N bytes of stderr. Independent
// records after a large error listing must still be able to prove corruption.
// Bound a single record and the unmatched-cause sets; overflow is inconclusive.
const maxDiagnosticRecordBytes = 1 << 20

type diagnosticStream struct {
	pending    []byte
	discarding bool
	evidence   diagnosticEvidence
	observe    func([]byte)
	onRecord   func(resticDiagnosticRecord)
	cancelled  func() bool
}

func (s *diagnosticStream) Write(data []byte) (int, error) {
	n := len(data)
	if s.observe != nil {
		s.observe(data)
	}
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		amount := len(data)
		if end >= 0 {
			amount = end + 1
		}
		if !s.discarding {
			if len(s.pending)+amount > maxDiagnosticRecordBytes {
				if s.cancelled != nil && s.cancelled() {
					s.evidence.cancelled = true
				}
				if !s.evidence.cancelled {
					s.evidence.incomplete = true
				}
				s.pending = nil
				s.discarding = true
			} else {
				s.pending = append(s.pending, data[:amount]...)
			}
		}
		data = data[amount:]
		if end >= 0 {
			if !s.discarding {
				s.consume(s.pending)
			}
			s.pending = nil
			s.discarding = false
		}
	}
	return n, nil
}
func (s *diagnosticStream) consume(line []byte) {
	for _, record := range parseResticDiagnostics(string(line)) {
		if s.cancelled != nil && s.cancelled() {
			s.evidence.cancelled = true
		}
		s.evidence.add(record)
		if s.onRecord != nil {
			s.onRecord(record)
		}
	}
}
func (s *diagnosticStream) finish() {
	if len(s.pending) > 0 && !s.discarding {
		s.consume(s.pending)
	}
	s.pending = nil
}
