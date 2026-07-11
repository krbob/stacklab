# Security Policy

Stacklab controls Docker on the host and can read or modify managed Compose
workspaces. Treat access to Stacklab as privileged host access and deploy it
only on a trusted network behind an appropriate transport-security boundary.

## Supported versions

Until the first stable release, security fixes target the latest released
version and the current default branch. Older pre-release builds are not
maintained. After a stable release, this section will list the supported
release lines explicitly.

## Reporting a vulnerability

Do not open a public issue for a suspected vulnerability and do not include
real credentials or host data in a report.

Use GitHub's private vulnerability reporting form for this repository:

<https://github.com/krbob/stacklab/security/advisories/new>

Include, when available:

- affected version or commit;
- deployment and proxy topology relevant to the finding;
- minimal reproduction steps or a proof of concept using synthetic data;
- expected impact and prerequisites;
- suggested mitigation, if known.

The maintainer will acknowledge the report, validate scope, coordinate a fix
and release, and credit the reporter when requested. Response and remediation
times depend on severity and maintainer availability; no fixed SLA is promised
for this pre-stable project.

## Security boundaries

The supported deployment is a single Linux host with authenticated, LAN-only
access. Reports are especially useful when they demonstrate a violation of a
documented boundary, including authentication or session bypass, filesystem
escape, secret disclosure, command injection, unsafe registry access, or a
release/update integrity failure.

Hardening requests that assume an internet-exposed multi-user service may be
handled as product proposals rather than vulnerabilities unless they bypass an
existing documented control.
