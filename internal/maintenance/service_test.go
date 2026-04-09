package maintenance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestServiceImagesAndPreview(t *testing.T) {
	service := NewService()
	service.runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "docker" {
			return nil, errors.New("unexpected command")
		}
		switch strings.Join(args, " ") {
		case "image ls --all --no-trunc --format {{json .}}":
			return []byte(strings.Join([]string{
				`{"ID":"sha256:used","Repository":"ghcr.io/example/app","Tag":"latest"}`,
				`{"ID":"sha256:unused","Repository":"ghcr.io/example/old","Tag":"1.0.0"}`,
			}, "\n")), nil
		case "image inspect sha256:used sha256:unused":
			return []byte(`[
				{"Id":"sha256:used","Created":"2026-04-04T12:11:00Z","Size":1000},
				{"Id":"sha256:unused","Created":"2026-04-03T12:11:00Z","Size":2000}
			]`), nil
		case "ps -aq":
			return []byte("container-1\n"), nil
		case "inspect container-1":
			return []byte(`[
				{
					"Image":"sha256:used",
					"Config":{
						"Image":"ghcr.io/example/app:latest",
						"Labels":{
							"com.docker.compose.project":"demo",
							"com.docker.compose.service":"app"
						}
					},
					"Mounts":[{"Name":"demo_data","Type":"volume"}],
					"NetworkSettings":{"Networks":{"demo_default":{},"external_shared":{}}}
				}
			]`), nil
		case "network ls --no-trunc --format {{json .}}":
			return []byte(strings.Join([]string{
				`{"ID":"network-demo","Name":"demo_default","Driver":"bridge","Scope":"local"}`,
				`{"ID":"network-ext","Name":"external_shared","Driver":"bridge","Scope":"local"}`,
			}, "\n")), nil
		case "network inspect network-demo network-ext":
			return []byte(`[
				{"Id":"network-demo","Name":"demo_default","Driver":"bridge","Scope":"local","Internal":false,"Attachable":false,"Ingress":false,"Labels":{"com.docker.compose.project":"demo"}},
				{"Id":"network-ext","Name":"external_shared","Driver":"bridge","Scope":"local","Internal":false,"Attachable":false,"Ingress":false,"Labels":{}}
			]`), nil
		case "volume ls --format {{json .}}":
			return []byte(strings.Join([]string{
				`{"Name":"demo_data","Driver":"local"}`,
				`{"Name":"external_media","Driver":"local"}`,
			}, "\n")), nil
		case "volume inspect demo_data external_media":
			return []byte(`[
				{"Name":"demo_data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/demo_data/_data","Scope":"local","Labels":{"com.docker.compose.project":"demo"},"Options":{}},
				{"Name":"external_media","Driver":"local","Mountpoint":"/var/lib/docker/volumes/external_media/_data","Scope":"local","Labels":{},"Options":{"type":"nfs"}}
			]`), nil
		case "system df --format {{json .}}":
			return []byte(strings.Join([]string{
				`{"Type":"Images","TotalCount":"2","Active":"1","Size":"3000B","Reclaimable":"2000B (66%)"}`,
				`{"Type":"Containers","TotalCount":"1","Active":"1","Size":"12kB","Reclaimable":"0B (0%)"}`,
				`{"Type":"Local Volumes","TotalCount":"0","Active":"0","Size":"0B","Reclaimable":"0B"}`,
				`{"Type":"Build Cache","TotalCount":"3","Active":"0","Size":"5MB","Reclaimable":"5MB"}`,
			}, "\n")), nil
		default:
			return nil, errors.New("unexpected args: " + strings.Join(args, " "))
		}
	}

	images, err := service.Images(context.Background(), ImagesQuery{
		Usage:           ImageUsageAll,
		Origin:          ImageOriginAll,
		ManagedStackIDs: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("Images() error = %v", err)
	}
	if len(images.Items) != 2 {
		t.Fatalf("Images() item count = %d, want 2", len(images.Items))
	}
	if images.Items[0].ID != "sha256:used" || images.Items[0].Source != ImageSourceStackManaged || images.Items[0].ContainersUsing != 1 {
		t.Fatalf("unexpected used image item: %#v", images.Items[0])
	}
	if images.Items[1].ID != "sha256:unused" || !images.Items[1].IsUnused || images.Items[1].Source != ImageSourceExternal {
		t.Fatalf("unexpected unused image item: %#v", images.Items[1])
	}
	if got := images.Items[0].CreatedAt.UTC(); !got.Equal(time.Date(2026, 4, 4, 12, 11, 0, 0, time.UTC)) {
		t.Fatalf("unexpected image created_at: %s", got)
	}

	networks, err := service.Networks(context.Background(), NetworksQuery{
		Usage:           ImageUsageAll,
		Origin:          ImageOriginAll,
		ManagedStackIDs: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("Networks() error = %v", err)
	}
	if len(networks.Items) != 2 {
		t.Fatalf("Networks() item count = %d, want 2", len(networks.Items))
	}
	if networks.Items[0].Name != "demo_default" || networks.Items[0].Source != NetworkSourceStackManaged {
		t.Fatalf("unexpected managed network item: %#v", networks.Items[0])
	}
	if networks.Items[1].Name != "external_shared" || networks.Items[1].Source != NetworkSourceStackManaged {
		t.Fatalf("unexpected external shared network item: %#v", networks.Items[1])
	}

	volumes, err := service.Volumes(context.Background(), VolumesQuery{
		Usage:           ImageUsageAll,
		Origin:          ImageOriginAll,
		ManagedStackIDs: []string{"demo"},
	})
	if err != nil {
		t.Fatalf("Volumes() error = %v", err)
	}
	if len(volumes.Items) != 2 {
		t.Fatalf("Volumes() item count = %d, want 2", len(volumes.Items))
	}
	if volumes.Items[0].Name != "demo_data" || volumes.Items[0].Source != VolumeSourceStackManaged {
		t.Fatalf("unexpected managed volume item: %#v", volumes.Items[0])
	}
	if volumes.Items[1].Name != "external_media" || !volumes.Items[1].IsUnused || volumes.Items[1].Source != VolumeSourceExternal {
		t.Fatalf("unexpected external volume item: %#v", volumes.Items[1])
	}

	preview, err := service.PrunePreview(context.Background(), PrunePreviewQuery{
		Images:            true,
		BuildCache:        true,
		StoppedContainers: true,
		Volumes:           true,
		ManagedStackIDs:   []string{"demo"},
	})
	if err != nil {
		t.Fatalf("PrunePreview() error = %v", err)
	}
	if preview.Preview.Images.Count != 1 || preview.Preview.Images.ReclaimableBytes != 2000 {
		t.Fatalf("unexpected image preview: %#v", preview.Preview.Images)
	}
	if preview.Preview.BuildCache.Count != 3 || preview.Preview.BuildCache.ReclaimableBytes == 0 {
		t.Fatalf("unexpected build cache preview: %#v", preview.Preview.BuildCache)
	}
	if preview.Preview.StoppedContainers.Count != 0 {
		t.Fatalf("unexpected stopped container preview: %#v", preview.Preview.StoppedContainers)
	}
	if preview.Preview.TotalReclaimableBytes == 0 {
		t.Fatalf("expected non-zero total reclaimable bytes")
	}
}

func TestParseDockerSize(t *testing.T) {
	cases := []struct {
		input string
		want  int64
	}{
		{"0B", 0},
		{"12kB", 12 * 1024},
		{"5MB", 5 * 1024 * 1024},
		{"92.82MB", 97328824},
		{"1.5GB", 1610612736},
	}
	for _, tc := range cases {
		got, err := parseDockerSize(tc.input)
		if err != nil {
			t.Fatalf("parseDockerSize(%q) error = %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("parseDockerSize(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}
