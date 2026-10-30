package backup

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"sync"
	"time"
)

// Version probing never holds the backup state mutex and never accesses a
// repository. Cache both unknown and known results to bound polling overhead.
type toolVersionCache struct {
	mu      sync.Mutex
	path    string
	info    os.FileInfo
	checked time.Time
	version string
}

var resticVersionPattern = regexp.MustCompile(`^restic ([0-9]+\.[0-9]+\.[0-9]+)(?:\s|$)`)

func (cache *toolVersionCache) restic(ctx context.Context) (bool, string) {
	path, err := exec.LookPath("restic")
	if err != nil {
		return false, ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, ""
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.path == path && cache.info != nil && os.SameFile(cache.info, info) && cache.info.ModTime() == info.ModTime() && cache.info.Size() == info.Size() && time.Since(cache.checked) < time.Minute {
		return true, cache.version
	}
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probe, path, "version")
	cmd.Env = cleanProcessEnvironment()
	cmd.WaitDelay = 100 * time.Millisecond
	var output versionOutput
	cmd.Stdout = &output
	version := ""
	if err := cmd.Run(); err == nil {
		if match := resticVersionPattern.FindSubmatch(output.Bytes()); match != nil {
			version = string(match[1])
		}
	}
	if ctx.Err() == nil {
		cache.path, cache.info, cache.checked, cache.version = path, info, time.Now(), version
	}
	return true, version
}

type versionOutput struct{ bytes.Buffer }

func (w *versionOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := 4096 - w.Len()
	if len(p) > remaining {
		p = p[:remaining]
	}
	_, _ = w.Buffer.Write(p)
	return n, nil
}
