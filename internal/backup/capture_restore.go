package backup

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"stacklab/internal/limitedio"
)

// ValidateArchive rejects extraction outside its target, link traversal and
// devices/FIFOs. Validation applies to every header before extraction begins.
func ValidateArchive(reader io.Reader, target string) error {
	archive := tar.NewReader(reader)
	links := map[string]bool{}
	seen := map[string]bool{}
	hasChildren := map[string]bool{}
	entries := 0
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(header.Name)
		entries++
		if entries > 1000000 || len(header.Name) > 4096 || len(header.Linkname) > 4096 {
			return errors.New("archive metadata exceeds supported entry or path limits")
		}
		if header.Uid < 0 || header.Gid < 0 || header.Mode < 0 || header.Mode&^07777 != 0 {
			return errors.New("archive contains invalid numeric ownership or permissions")
		}
		// Repository contents are untrusted. Root extraction may preserve ordinary
		// ownership, permissions, ACLs and xattrs, but must not manufacture an
		// executable that grants privileges to the unprivileged service identity.
		if header.Mode&04000 != 0 || header.Mode&02000 != 0 && header.Typeflag != tar.TypeDir {
			return errors.New("archive contains unsupported setuid or setgid file permissions")
		}
		if _, ok := header.Xattrs["security.capability"]; ok {
			return errors.New("archive contains unsupported file capabilities")
		}
		for key := range header.PAXRecords {
			if strings.HasSuffix(key, ".security.capability") {
				return errors.New("archive contains unsupported file capabilities")
			}
		}
		if strings.ContainsRune(name, 0) || strings.Contains(name, "\\") || path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return errors.New("archive contains an unsafe path")
		}
		if name == "." && header.Typeflag != tar.TypeDir {
			return errors.New("archive root must be a directory")
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			hasChildren[parent] = true
			if links[parent] {
				return errors.New("archive writes through a link")
			}
		}
		if links[name] {
			return errors.New("archive replaces a link")
		}
		switch header.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		case tar.TypeSymlink:
			resolved := path.Clean(path.Join(path.Dir(name), header.Linkname))
			if path.IsAbs(header.Linkname) || resolved == ".." || strings.HasPrefix(resolved, "../") {
				return errors.New("archive symlink escapes destination")
			}
			if hasChildren[name] {
				return errors.New("archive replaces a populated directory with a symlink")
			}
			links[name] = true
		case tar.TypeLink:
			link := path.Clean(header.Linkname)
			if path.IsAbs(link) || link == ".." || strings.HasPrefix(link, "../") || !seen[link] || links[link] {
				return errors.New("archive contains an unsafe hard link")
			}
			for parent := path.Dir(link); parent != "."; parent = path.Dir(parent) {
				if links[parent] {
					return errors.New("archive hard link traverses a symlink")
				}
			}
			links[name] = true
		default:
			return errors.New("archive contains unsupported special file metadata")
		}
		if target != "" {
			current := target
			for _, part := range strings.Split(name, "/") {
				if part == "." {
					continue
				}
				current = filepath.Join(current, part)
				info, statErr := os.Lstat(current)
				if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
					return statErr
				}
				// The non-directory tar pass uses --unlink-first to replace this
				// final link without following it. Permit repeated recovery, while
				// retaining the strict prohibition on traversing link ancestors.
				replacingFinalSymlink := current == filepath.Join(target, filepath.FromSlash(name)) && header.Typeflag == tar.TypeSymlink
				if statErr == nil && info.Mode()&os.ModeSymlink != 0 && !replacingFinalSymlink {
					return fmt.Errorf("restore target contains a symlink: %s", name)
				}
				final := current == filepath.Join(target, filepath.FromSlash(name))
				if statErr == nil && (info.IsDir() && final && header.Typeflag != tar.TypeDir || !info.IsDir() && (!final || header.Typeflag == tar.TypeDir)) {
					return fmt.Errorf("restore target has a conflicting file or directory: %s", name)
				}
				if statErr == nil && !info.IsDir() && !info.Mode().IsRegular() && !(replacingFinalSymlink && info.Mode()&os.ModeSymlink != 0) {
					return errors.New("restore target contains a special file")
				}
			}
		}
		seen[name] = true
	}
}

// ArchiveArguments is shared with the narrowly scoped native helper. It never
// invokes a shell, dereferences symlinks or accepts raw command options.
func ArchiveArguments(source string, excludes []string) ([]string, error) {
	return archiveArgs(source, excludes)
}

func (c *NativeCapture) RestoreArchive(ctx context.Context, archivePath, targetPath string, privileged bool) error {
	if !filepath.IsAbs(targetPath) {
		return errors.New("restore target must be an absolute directory")
	}
	if privileged {
		if c.cfg.BackupHelperPath == "" {
			return errors.New("preserving ownership during in-place restore requires the backup helper")
		}
		args := []string{"restore", "--path", targetPath, "--archive", archivePath}
		name := c.cfg.BackupHelperPath
		if c.cfg.BackupUseSudo {
			args = append([]string{"-n", name}, args...)
			name = "sudo"
		}
		_, err := c.command(ctx, name, args...)
		return err
	}
	return ExtractArchive(ctx, archivePath, targetPath, false)
}

// ExtractArchive validates the complete archive before invoking native tar.
// Fresh targets are private. In-place extraction is reserved for helper calls
// after its independent target authorization.
func ExtractArchive(ctx context.Context, archivePath, targetPath string, preserveOwner bool) error {
	current := filepath.Clean(targetPath)
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return errors.New("restore target must not contain symlinks")
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	if err := os.MkdirAll(targetPath, 0700); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return err
	}
	if filepath.Clean(abs) != resolved {
		return errors.New("restore target must not contain symlinks")
	}
	var pinnedDirectory *os.File
	tarTarget := targetPath
	if runtime.GOOS == "linux" {
		pinnedDirectory, err = os.Open(targetPath)
		if err != nil {
			return err
		}
		defer pinnedDirectory.Close()
		actual, linkErr := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", pinnedDirectory.Fd()))
		if linkErr != nil {
			return linkErr
		}
		if actual != resolved {
			return errors.New("restore directory changed during validation")
		}
		tarTarget = "/proc/self/fd/3"
	}
	return extractArchivePinned(ctx, archivePath, targetPath, tarTarget, pinnedDirectory, preserveOwner)
}

// ExtractArchivePinned keeps every extraction pass on the directory descriptor
// already authorized by the helper, even if its pathname is renamed or replaced.
func ExtractArchivePinned(ctx context.Context, archivePath string, directory *os.File, preserveOwner bool) error {
	if runtime.GOOS != "linux" {
		return errors.New("descriptor-pinned privileged extraction requires Linux")
	}
	validationTarget := fmt.Sprintf("/proc/self/fd/%d", directory.Fd())
	return extractArchivePinned(ctx, archivePath, validationTarget, "/proc/self/fd/3", directory, preserveOwner)
}

func extractArchivePinned(ctx context.Context, archivePath, targetPath, tarTarget string, pinnedDirectory *os.File, preserveOwner bool) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = ValidateArchive(captureContextReader{ctx: ctx, reader: file}, targetPath); err != nil {
		return err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	members, directories, cleanup, err := archiveMemberLists(ctx, file)
	if err != nil {
		return err
	}
	defer cleanup()
	args := []string{"--extract", "--file=-", "--directory=" + tarTarget, "--acls", "--xattrs", "--xattrs-include=*", "--numeric-owner", "--same-permissions", "--delay-directory-restore", "--no-recursion", "--no-wildcards", "--anchored", "--null", "--verbatim-files-from"}
	if preserveOwner {
		args = append(args, "--same-owner")
	} else {
		args = append(args, "--no-same-owner")
	}
	// GNU tar --unlink-first also tries to unlink existing directories, which
	// breaks merges into non-empty directories. Retain its safe replacement of
	// file/hardlink inodes in the first pass, then apply directory metadata only.
	// --no-recursion is essential: a named directory must not select its files
	// again without --unlink-first, including existing links outside the target.
	for index, selection := range []string{members, directories} {
		selectionInfo, statErr := os.Stat(selection)
		if statErr != nil {
			return statErr
		}
		// Some GNU tar builds interpret an empty --files-from as selecting the
		// entire archive. Never run an empty pass: --unlink-first must not receive
		// directory entries, including the extraction root ".".
		if selectionInfo.Size() == 0 {
			continue
		}
		if _, err = file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		passArgs := append(append([]string{}, args...), "--files-from="+selection)
		if index == 0 {
			passArgs = append(passArgs, "--unlink-first")
		}
		cmd := exec.CommandContext(ctx, tarExecutable(), passArgs...)
		configureCaptureProcess(cmd)
		if pinnedDirectory != nil {
			cmd.ExtraFiles = []*os.File{pinnedDirectory}
		}
		cmd.Stdin = file
		output := limitedio.NewBuffer(32 * 1024)
		cmd.Stdout = output
		cmd.Stderr = output
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
		if err = cmd.Run(); err != nil {
			return fmt.Errorf("native extraction failed: %s: %w", strings.TrimSpace(output.String()), err)
		}
	}
	return nil
}

// Lists are private, NUL-delimited and passed as verbatim members. This preserves
// native GNU tar sparse/ACL/xattr handling without rebuilding the archive or
// accepting member names as command options. The complete archive is validated
// before this helper is reached and before any target data is touched.
func archiveMemberLists(ctx context.Context, archive *os.File) (string, string, func(), error) {
	files := []*os.File{}
	cleanup := func() {
		for _, file := range files {
			_ = file.Close()
			_ = os.Remove(file.Name())
		}
	}
	for range 2 {
		file, err := os.CreateTemp(filepath.Dir(archive.Name()), ".stacklab-recovery-members-*")
		if err != nil {
			cleanup()
			return "", "", nil, err
		}
		files = append(files, file)
	}
	reader := tar.NewReader(contextReader{ctx: ctx, reader: archive})
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			cleanup()
			return "", "", nil, err
		}
		index := 0
		if header.Typeflag == tar.TypeDir {
			index = 1
		}
		if _, err = io.WriteString(files[index], header.Name+"\x00"); err != nil {
			cleanup()
			return "", "", nil, err
		}
	}
	for _, file := range files {
		if err := file.Close(); err != nil {
			cleanup()
			return "", "", nil, err
		}
	}
	return files[0].Name(), files[1].Name(), cleanup, nil
}

type RestoreMapping struct {
	SourceID   string `json:"source_id"`
	Target     string `json:"target"`
	VolumeName string `json:"volume_name,omitempty"`
	Mode       string `json:"mode,omitempty"`
}
type RestorePreview struct {
	Mappings   []RestoreMapping `json:"mappings"`
	Writers    []Writer         `json:"writers"`
	Warnings   []string         `json:"warnings"`
	Overwrites bool             `json:"overwrites"`
}

// PreviewRestore requires a mapping for each selected source; Stacklab state
// is recovered offline because replacing the running SQLite database is unsafe.
func (c *NativeCapture) PreviewRestore(ctx context.Context, manifest Manifest, mappings []RestoreMapping) (RestorePreview, error) {
	preview := RestorePreview{Mappings: append([]RestoreMapping{}, mappings...), Writers: []Writer{}, Warnings: []string{"Complete-source restore replaces all content inside each selected target by default; files absent from the archive are removed. Explicit merge retains unrelated files. Replacement preserves the target directory/mountpoint and is not atomic across sources. Failed replacements retain recovery records and rollback data for inspection. Targets are extraction directories; file sources restore their original basename. Services remain stopped after an interrupted or failed write; starting previously running services requires explicit selection."}}
	if manifest.Version != 1 {
		return preview, errors.New("unsupported recovery manifest version")
	}
	if len(mappings) == 0 {
		return preview, errors.New("restore requires explicit source mappings")
	}
	if c.cfg.BackupHelperPath == "" {
		return preview, errors.New("in-place recovery requires the configured backup permission helper")
	}
	sourceByID := map[string]ManifestSource{}
	for _, source := range manifest.Sources {
		sourceByID[source.Source.ID] = source
	}
	containers, err := c.containers(ctx)
	if err != nil {
		return preview, err
	}
	seen := map[string]bool{}
	for index, mapping := range mappings {
		if mapping.Mode == "" {
			mapping.Mode = "replace"
		}
		if mapping.Mode != "replace" && mapping.Mode != "merge" {
			return preview, errors.New("restore mode must be replace or merge")
		}
		preview.Mappings[index].Mode = mapping.Mode
		source, ok := sourceByID[mapping.SourceID]
		if !ok {
			return preview, errors.New("unknown restore source")
		}
		if source.FileName != "" {
			// A file source maps to its containing directory. Replacing that entire
			// directory would delete unrelated siblings which were never captured.
			mapping.Mode = "merge"
			preview.Mappings[index].Mode = "merge"
			preview.Warnings = append(preview.Warnings, "Single-file source "+source.Source.ID+" replaces only its archived basename; neighboring files are retained.")
		}
		if len(source.RecoveryRestrictions) > 0 {
			return preview, errors.New("source requires operator-controlled offline recovery: " + strings.Join(source.RecoveryRestrictions, "; "))
		}
		if source.Source.Kind == "stacklab" {
			return preview, errors.New("Stacklab state must be recovered offline using the documented procedure")
		}
		if seen[mapping.SourceID] {
			return preview, errors.New("duplicate restore mapping")
		}
		seen[mapping.SourceID] = true
		if mapping.VolumeName != "" {
			output, inspectErr := c.command(ctx, "docker", "volume", "inspect", mapping.VolumeName)
			if inspectErr != nil {
				return preview, errors.New("create and review the destination volume before restoring")
			}
			var volumes []captureVolume
			if jsonErr := json.Unmarshal(output, &volumes); jsonErr != nil || len(volumes) != 1 || volumes[0].Driver != "local" || len(volumes[0].Options) > 0 {
				return preview, errors.New("restore requires a local volume without driver options")
			}
			mapping.Target = volumes[0].Mountpoint
			preview.Mappings[index].Target = mapping.Target
			preview.Overwrites = true
		}
		if !filepath.IsAbs(mapping.Target) || filepath.Clean(mapping.Target) == "/" {
			return preview, errors.New("invalid restore target")
		}
		for previous := 0; previous < index; previous++ {
			if pathsOverlapData(preview.Mappings[previous].Target, mapping.Target) {
				return preview, errors.New("restore target mappings must not overlap")
			}
		}
		allowed := mapping.VolumeName != ""
		roots := append([]string{filepath.Join(c.cfg.RootDir, "stacks"), filepath.Join(c.cfg.RootDir, "config"), filepath.Join(c.cfg.RootDir, "data")}, c.cfg.BackupAllowedPaths...)
		for _, root := range roots {
			if filepath.IsAbs(root) && pathContains(normalizedDataPath(root), normalizedDataPath(mapping.Target)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return preview, errors.New("restore target must be a managed stacks/config/data directory, an inspected local volume, or a configured backup allowed path")
		}
		checkArgs := []string{"check-target", "--path", mapping.Target}
		if mapping.VolumeName != "" {
			checkArgs = []string{"check-target", "--volume", mapping.VolumeName}
		}
		name, args := c.helperInvocation(checkArgs...)
		if _, checkErr := c.command(ctx, name, args...); checkErr != nil {
			return preview, fmt.Errorf("restore helper rejected the target; check configured roots, symlinks and permissions: %w", checkErr)
		}
		if pathsOverlapData(mapping.Target, c.cfg.DataDir) {
			return preview, errors.New("online restore cannot overwrite private Stacklab state")
		}
		if info, statErr := os.Lstat(mapping.Target); statErr == nil && !info.IsDir() {
			return preview, errors.New("restore target must be a directory, not a file or symlink")
		}
		if _, err = os.Lstat(mapping.Target); err == nil || errors.Is(err, os.ErrPermission) {
			preview.Overwrites = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return preview, err
		}
		for _, container := range containers {
			for _, mount := range container.Mounts {
				if mount.RW && pathsOverlapData(mapping.Target, mount.Source) {
					if container.State.Paused {
						return preview, errors.New("restore writer is paused")
					}
					preview.Writers = append(preview.Writers, Writer{ContainerID: container.ID, StackID: container.Config.Labels["com.docker.compose.project"], Service: container.Config.Labels["com.docker.compose.service"], Running: container.State.Running, StopTimeoutSeconds: container.Config.StopTimeout})
					break
				}
			}
		}
	}
	for _, writer := range preview.Writers {
		if writer.Running {
			if _, err = writerStopTimeout(writer); err != nil {
				return preview, err
			}
		}
	}
	return preview, nil
}

func (c *NativeCapture) prepareRestoreArchive(ctx context.Context, archive string, mapping RestoreMapping) (string, error) {
	input, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	args := []string{"prepare-restore", "--path", mapping.Target, "--mode", mapping.Mode, "--size", fmt.Sprint(info.Size())}
	name, args := c.helperInvocation(args...)
	cmd := exec.CommandContext(ctx, name, args...)
	configureCaptureProcess(cmd)
	cmd.Stdin = input
	output := limitedio.NewBuffer(4096)
	diagnostic := limitedio.NewBuffer(8192)
	cmd.Stdout = output
	cmd.Stderr = diagnostic
	if err = cmd.Run(); err != nil {
		return "", fmt.Errorf("recovery preflight failed: %s: %w", strings.TrimSpace(diagnostic.String()), err)
	}
	token := strings.TrimSpace(output.String())
	if len(token) != 48 || strings.Trim(token, "0123456789abcdef") != "" {
		return "", errors.New("helper returned an invalid prepared recovery token")
	}
	return token, nil
}

func (c *NativeCapture) preparedRestoreCommand(ctx context.Context, action, token string) error {
	name, args := c.helperInvocation(action, "--token", token)
	output, err := c.command(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("%s failed: %s: %w", action, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (c *NativeCapture) checkPreparedRestores(ctx context.Context, mappings []RestoreMapping, tokens map[string]string) error {
	args := []string{"check-restores"}
	for _, mapping := range mappings {
		args = append(args, "--token", tokens[mapping.SourceID])
	}
	name, args := c.helperInvocation(args...)
	output, err := c.command(ctx, name, args...)
	if err != nil {
		return fmt.Errorf("recovery preflight failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (c *NativeCapture) ApplyRestore(ctx context.Context, manifest Manifest, mappings []RestoreMapping, restoredDirectory, runID string, resumeServices bool) (err error) {
	if err = c.Recover(ctx); err != nil {
		return err
	}
	preview, err := c.PreviewRestore(ctx, manifest, mappings)
	if err != nil {
		return err
	}
	sources := map[string]ManifestSource{}
	for _, source := range manifest.Sources {
		sources[source.Source.ID] = source
	}
	tokens := map[string]string{}
	keepPrepared := false
	defer func() {
		if keepPrepared {
			return
		}
		releaseCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, token := range tokens {
			err = errors.Join(err, c.preparedRestoreCommand(releaseCtx, "release-restore", token))
		}
	}()
	// The root helper reads caller-opened bytes through stdin. Custom isolated
	// targets are supported without granting arbitrary root archive reads.
	for _, mapping := range preview.Mappings {
		archive, pathErr := ManifestArchivePath(restoredDirectory, sources[mapping.SourceID])
		if pathErr != nil {
			return pathErr
		}
		token, prepareErr := c.prepareRestoreArchive(ctx, archive, mapping)
		if prepareErr != nil {
			return prepareErr
		}
		tokens[mapping.SourceID] = token
	}
	if err = c.checkPreparedRestores(ctx, preview.Mappings, tokens); err != nil {
		return err
	}
	writers := uniqueRunningWriters([]ManifestSource{{Writers: preview.Writers}})
	journal := newStopJournal(runID, writers)
	journal.PreparedTokens = tokens
	if err = c.saveStopJournal(journal); err != nil {
		return err
	}
	writesStarted := false
	defer func() {
		if !writesStarted || err == nil && resumeServices {
			resumeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			journal.HoldStopped = false
			err = errors.Join(err, c.resume(resumeCtx, journal))
		} else if err == nil {
			err = c.removeJournal(runID)
		} else {
			keepPrepared = true
			err = fmt.Errorf("restore incomplete; affected services remain stopped; recovery journal and helper rollback records are retained: %w", err)
		}
	}()
	for _, writer := range writers {
		if err = c.stopJournalWriter(ctx, &journal, writer); err != nil {
			return err
		}
	}
	// Recheck every target and combined filesystem budget after writers stop,
	// before the durable write intent. Inputs are immutable root-owned archives.
	if err = c.checkPreparedRestores(ctx, preview.Mappings, tokens); err != nil {
		return err
	}
	journal.HoldStopped = true
	if err = c.saveStopJournal(journal); err != nil {
		return err
	}
	writesStarted = true
	for _, mapping := range preview.Mappings {
		if err = c.preparedRestoreCommand(ctx, "apply-restore", tokens[mapping.SourceID]); err != nil {
			return err
		}
	}
	for _, mapping := range preview.Mappings {
		if sources[mapping.SourceID].Source.Kind == "stack_definition" {
			if _, err = c.command(ctx, "docker", "compose", "--project-directory", mapping.Target, "-f", filepath.Join(mapping.Target, "compose.yaml"), "config", "--quiet"); err != nil {
				return errors.New("restored Compose definition failed validation")
			}
		}
	}
	return nil
}

// ManifestArchivePath treats repository metadata as untrusted input.
func ManifestArchivePath(root string, source ManifestSource) (string, error) {
	if !source.Archive || source.SavedPath == "" || filepath.IsAbs(source.SavedPath) {
		return "", errors.New("invalid manifest archive path")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	archive := filepath.Join(root, filepath.FromSlash(source.SavedPath))
	if archive == root || !pathContains(root, archive) {
		return "", errors.New("manifest archive path escapes recovery directory")
	}
	resolved, err := filepath.EvalSymlinks(archive)
	if err != nil {
		return "", err
	}
	if resolved != archive {
		return "", errors.New("manifest archive path contains symlinks")
	}
	info, err := os.Lstat(archive)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("manifest archive must be a regular file")
	}
	return archive, nil
}
