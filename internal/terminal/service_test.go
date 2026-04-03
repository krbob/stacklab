package terminal

import "testing"

func TestNormalizeShell(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "default", input: "", want: "/bin/sh"},
		{name: "sh", input: "/bin/sh", want: "/bin/sh"},
		{name: "bash", input: "/bin/bash", want: "/bin/bash"},
		{name: "invalid", input: "/bin/zsh", wantErr: true},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := normalizeShell(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeShell(%q) error = nil, want error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeShell(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("normalizeShell(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}
