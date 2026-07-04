package stacks

import (
	"bytes"
	"strings"
	"testing"
)

func TestConsumeComposeProgress(t *testing.T) {
	input := strings.Join([]string{
		`{"id":"0d6922a6b13e","text":"Pulling fs layer","status":"Working"}`,
		`{"id":"f6a4c3e338ed","text":"Pulling fs layer","status":"Working"}`,
		`{"id":"0d6922a6b13e","text":"Pull complete","status":"Done"}`,
		`plain stderr warning line`,
		`{"id":"f6a4c3e338ed","text":"Pull complete","status":"Done"}`,
	}, "\n")

	var text bytes.Buffer
	var updates []StepProgress
	consumeComposeProgress(strings.NewReader(input), &text, func(p StepProgress) {
		updates = append(updates, p)
	})

	if len(updates) == 0 {
		t.Fatal("no progress updates emitted")
	}
	final := updates[len(updates)-1]
	if final.Completed != 2 || final.Total != 2 {
		t.Fatalf("final progress = %+v, want 2/2", final)
	}
	if !strings.Contains(final.Detail, "Pull complete") {
		t.Fatalf("detail = %q", final.Detail)
	}
	if !strings.Contains(text.String(), "plain stderr warning line") {
		t.Fatalf("non-JSON line lost: %q", text.String())
	}
}

func TestConsumeComposeProgressPlainOutputOnly(t *testing.T) {
	var text bytes.Buffer
	called := false
	consumeComposeProgress(strings.NewReader("some error\nanother line\n"), &text, func(StepProgress) { called = true })

	if called {
		t.Fatal("progress emitted for plain output")
	}
	if !strings.Contains(text.String(), "another line") {
		t.Fatalf("text = %q", text.String())
	}
}
