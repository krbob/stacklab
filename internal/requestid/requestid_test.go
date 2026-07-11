package requestid

import (
	"context"
	"strings"
	"testing"
)

func TestResolvePreservesSafeCallerID(t *testing.T) {
	t.Parallel()

	const supplied = "edge-01:req_123.test"
	if got := Resolve(supplied); got != supplied {
		t.Fatalf("Resolve(%q) = %q", supplied, got)
	}
}

func TestResolveReplacesUnsafeCallerID(t *testing.T) {
	t.Parallel()

	for _, supplied := range []string{"", "contains spaces", "line\nbreak", strings.Repeat("a", 129)} {
		resolved := Resolve(supplied)
		if resolved == supplied || !validID.MatchString(resolved) {
			t.Fatalf("Resolve(%q) = %q, want a new safe ID", supplied, resolved)
		}
	}
}

func TestContextRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := WithContext(context.Background(), "req_context")
	if got := FromContext(ctx); got != "req_context" {
		t.Fatalf("FromContext() = %q", got)
	}
	if got := FromContext(context.Background()); got != "" {
		t.Fatalf("FromContext(empty) = %q", got)
	}
}
