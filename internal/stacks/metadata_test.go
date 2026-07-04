package stacks

import "testing"

func TestParseComposeMetadata(t *testing.T) {
	content := []byte(`
services:
  app:
    image: ghcr.io/example/app:1.0
x-stacklab:
  icon: jellyfin
  links:
    - label: Web UI
      url: https://jellyfin.example.net
    - label: Docs
      url: https://docs.example.net/path?x=1
`)

	_, meta, err := parseComposeServices("/tmp/stack", content)
	if err != nil {
		t.Fatalf("parseComposeServices() error = %v", err)
	}
	if meta == nil {
		t.Fatal("metadata = nil, want parsed block")
	}
	if meta.Icon != "jellyfin" {
		t.Fatalf("icon = %q, want jellyfin", meta.Icon)
	}
	if len(meta.Links) != 2 {
		t.Fatalf("len(links) = %d, want 2", len(meta.Links))
	}
	if meta.Links[0].Label != "Web UI" || meta.Links[0].URL != "https://jellyfin.example.net" {
		t.Fatalf("links[0] = %+v", meta.Links[0])
	}
}

func TestParseComposeMetadataAbsent(t *testing.T) {
	content := []byte("services:\n  app:\n    image: nginx\n")
	_, meta, err := parseComposeServices("/tmp/stack", content)
	if err != nil {
		t.Fatalf("parseComposeServices() error = %v", err)
	}
	if meta != nil {
		t.Fatalf("metadata = %+v, want nil", meta)
	}
}

func TestParseStackMetadataValidation(t *testing.T) {
	cases := []struct {
		name string
		in   composeXStacklab
		want *StackMetadata
	}{
		{
			name: "invalid icon dropped",
			in:   composeXStacklab{Icon: "Bad Icon!"},
			want: nil,
		},
		{
			name: "non-http link dropped",
			in: composeXStacklab{Links: []struct {
				Label string `yaml:"label"`
				URL   string `yaml:"url"`
			}{{Label: "ssh", URL: "ssh://host"}, {Label: "", URL: "https://ok.example"}}},
			want: nil,
		},
		{
			name: "valid icon only",
			in:   composeXStacklab{Icon: "uptime-kuma"},
			want: &StackMetadata{Icon: "uptime-kuma"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseStackMetadata(&tc.in)
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
			if got != nil && got.Icon != tc.want.Icon {
				t.Fatalf("icon = %q, want %q", got.Icon, tc.want.Icon)
			}
		})
	}
}

func TestParseStackMetadataLinkCap(t *testing.T) {
	links := make([]struct {
		Label string `yaml:"label"`
		URL   string `yaml:"url"`
	}, 12)
	for i := range links {
		links[i].Label = "L"
		links[i].URL = "https://example.net"
	}
	got := parseStackMetadata(&composeXStacklab{Links: links})
	if got == nil || len(got.Links) != metadataMaxLinks {
		t.Fatalf("links = %v, want %d entries", got, metadataMaxLinks)
	}
}
