# Filesystem Metadata and Atomic Writes

Stacklab replaces managed text files through a temporary file in the same
directory, `fsync(2)`, `rename(2)`, and a directory `fsync(2)`. A successful
return therefore means both file contents and the renamed directory entry have
been submitted to durable storage.

## Existing files

For an existing regular file, an atomic replacement preserves:

- numeric owner UID and group GID;
- Unix permission and special mode bits;
- the exact supported extended-attribute set, including removal of attributes
  inherited by the temporary file but absent from the destination;
- Linux POSIX access ACLs, which are represented by
  `system.posix_acl_access`.

Metadata preservation is strict. If Stacklab can replace the contents but
cannot read or restore owner, group, or an advertised extended attribute, the
write fails before `rename(2)` and the original file remains in place. Extended
metadata is bounded to 1 MiB per file.

Modification/change timestamps and inode identity intentionally change with
the replacement; atomic writes do not preserve hard-link identity.

`WriteStringMode` and `WriteBytesMode` preserve owner, group, and extended
metadata while deliberately replacing the permission bits. Mode is applied
after ownership and ACL restoration, so it is authoritative and updates the
ACL mask consistently.

## New files

A new file receives the process owner/group, the requested mode (or `0644` for
the default API), and any default ACL or security metadata inherited when the
temporary file is created in the destination directory.

## Platform boundary

Owner/group and xattr copying is implemented on Linux and macOS. Linux POSIX
ACLs are covered through their xattr representation. ACL models that are not
exposed as extended attributes by the host are outside the portable guarantee;
operators using such a filesystem should validate effective ACLs before
enabling writes. Debian-family Linux remains the production target.

## Docker daemon helper

The privileged Docker admin helper uses the same atomic-write implementation
for apply and rollback. It fsyncs the staged `daemon.json` before rename and
fsyncs `/etc/docker` after rename or removal.

Backups are created with `O_EXCL`, mode `0600`, nanosecond timestamps, and a
collision suffix. An existing backup is never overwritten. The helper fsyncs
both the backup file and backup directory before reporting its path.
