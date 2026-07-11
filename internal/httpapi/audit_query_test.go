package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseAuditListQuery(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest("GET", "http://stacklab.test/api/audit?q=restart&result=failed&from=2026-07-01T22:00:00%2B02:00&to=2026-07-12T00:00:00Z&limit=25&cursor=next", nil)
	query, err := parseAuditListQuery(request, "demo")
	if err != nil {
		t.Fatalf("parseAuditListQuery() error = %v", err)
	}
	if query.StackID != "demo" || query.Search != "restart" || query.Cursor != "next" || query.Limit != 25 {
		t.Fatalf("parsed scalar filters = %#v", query)
	}
	if len(query.Results) != 2 || query.Results[0] != "failed" || query.Results[1] != "timed_out" {
		t.Fatalf("parsed results = %#v", query.Results)
	}
	wantFrom := time.Date(2026, 7, 1, 20, 0, 0, 0, time.UTC)
	wantBefore := time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC)
	if query.RequestedFrom == nil || !query.RequestedFrom.Equal(wantFrom) {
		t.Fatalf("parsed from = %v, want %v", query.RequestedFrom, wantFrom)
	}
	if query.RequestedBefore == nil || !query.RequestedBefore.Equal(wantBefore) {
		t.Fatalf("parsed before = %v, want %v", query.RequestedBefore, wantBefore)
	}
}

func TestParseAuditListQueryRejectsInvalidFilters(t *testing.T) {
	t.Parallel()

	for _, rawQuery := range []string{
		"result=unknown",
		"from=2026-07-01",
		"from=not-a-timestamp",
		"from=2026-07-12T00:00:00Z&to=2026-07-11T00:00:00Z",
		"q=" + strings.Repeat("x", 201),
	} {
		request := httptest.NewRequest("GET", "http://stacklab.test/api/audit?"+rawQuery, nil)
		if _, err := parseAuditListQuery(request, ""); err == nil {
			t.Fatalf("parseAuditListQuery(%q) error = nil", rawQuery)
		}
	}
}
