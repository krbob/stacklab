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
	}, nil)

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
	consumeComposeProgress(strings.NewReader("some error\nanother line\n"), &text, func(StepProgress) { called = true }, nil)

	if called {
		t.Fatal("progress emitted for plain output")
	}
	if !strings.Contains(text.String(), "another line") {
		t.Fatalf("text = %q", text.String())
	}
}

func TestConsumePlainProgressPull(t *testing.T) {
	input := strings.Join([]string{
		" adguardhome Pulling ",
		" 0d6922a6b13e Pulling fs layer ",
		" f6a4c3e338ed Pulling fs layer ",
		" 0d6922a6b13e Pull complete ",
		" f6a4c3e338ed Already exists ",
		" adguardhome Pulled ",
	}, "\n")

	var text bytes.Buffer
	var updates []StepProgress
	consumePlainProgress(strings.NewReader(input), &text, func(p StepProgress) {
		updates = append(updates, p)
	}, nil)

	if len(updates) == 0 {
		t.Fatal("no updates")
	}
	final := updates[len(updates)-1]
	if final.Completed != 2 || final.Total != 2 {
		t.Fatalf("final = %+v, want 2/2", final)
	}
	if !strings.Contains(text.String(), "adguardhome Pulled") {
		t.Fatalf("text lost lines: %q", text.String())
	}
}

func TestConsumePlainProgressContainers(t *testing.T) {
	input := "Container adguardhome Recreate\nContainer adguardhome Recreated\nContainer adguardhome Starting\nContainer adguardhome Started\n"
	var text bytes.Buffer
	var updates []StepProgress
	consumePlainProgress(strings.NewReader(input), &text, func(p StepProgress) {
		updates = append(updates, p)
	}, nil)
	final := updates[len(updates)-1]
	if final.Completed != 1 || final.Total != 1 {
		t.Fatalf("final = %+v, want 1/1", final)
	}
}
