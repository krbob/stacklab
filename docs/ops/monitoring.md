# Prometheus And Grafana Monitoring

Stacklab can join an existing single-host monitoring stack:

- node_exporter observes host CPU, RAM, filesystems, disks, network, and sensors;
- cAdvisor observes Docker container CPU, working-set memory, network, and OOMs;
- Stacklab's authenticated `/metrics` endpoint observes the application itself;
- Prometheus retains history and Grafana displays it. Stacklab does not store a
  second copy of monitoring history.

The versioned [dashboard](../../deploy/monitoring/stacklab-overview.json) has UID
`stacklab-overview`. It uses the shared `host` target label and lets the operator
filter Docker series by Compose project. Existing dashboards and data sources
can be kept. The default Prometheus datasource UID is `prometheus`; a datasource
selector permits another instance.

## Repeatable Setup From A Release Or Git

[setup.py](../../scripts/monitoring/setup.py) configures monitoring on a local,
rootful Linux Docker Engine host with Compose v2 (`docker compose` or
`docker-compose`), running
Stacklab as a systemd service. It supports
both package and tarball installations. It does not install or replace Stacklab;
install a release that supports `/metrics` first.

The tool and all templates ship in release archives under `monitoring/` and in
Debian packages under `/usr/lib/stacklab/monitoring/`. Python 3.9 or newer and
PyYAML are required. Debian packages recommend `python3` and `python3-yaml`; if
recommendations were disabled, install them explicitly:

```bash
sudo apt-get install python3 python3-yaml
```

### New Monitoring Stack

Copy [setup.example.json](../../deploy/monitoring/setup.example.json) into your
workspace, for example `/srv/stacklab/monitoring-setup.json`. Set `host_label` to
the name to display in Grafana. The example uses the packaged workspace and
Stacklab's default listener. For a tarball install, set `workspace` to your actual
root, normally `/opt/stacklab`; `state_dir` normally remains `/var/lib/stacklab`.

Preview and apply from a package installation:

```bash
sudo python3 /usr/lib/stacklab/monitoring/setup.py --config /srv/stacklab/monitoring-setup.json
sudo python3 /usr/lib/stacklab/monitoring/setup.py --config /srv/stacklab/monitoring-setup.json --apply --deploy
```

From a Git checkout, use `python3 scripts/monitoring/setup.py`; from an extracted
release, use `python3 monitoring/setup.py`, with the same arguments. The default
invocation only previews file changes. `--apply` writes configuration and enables
Stacklab metrics; `--deploy` additionally starts containers and verifies the
scrape targets. Without `--deploy`, the generated stack can be deployed through
Stacklab's existing stack actions.

Standalone mode creates Prometheus, Grafana, node_exporter and cAdvisor with
pinned image versions. They use host networking with loopback listeners, so the
default `http://127.0.0.1:8080/metrics` is reachable without changing Stacklab's
listener. Ports default to Grafana 3000, Prometheus 9090, node_exporter 9100, and
cAdvisor 8081. These ports must be available on the new host.

Grafana grants anonymous Viewer access and listens on loopback. For remote access,
forward its port over SSH:

```bash
ssh -L 3000:127.0.0.1:3000 user@server
```

Then open `http://localhost:3000/d/stacklab-overview`. To use a reverse proxy,
configure `grafana_listen` with a host address reachable from that proxy and
configure the proxy separately. The generated stack has no dependency on a
particular domain, Traefik network, or certificate resolver.

### Existing Prometheus And Grafana

Start from [setup.existing.example.json](../../deploy/monitoring/setup.existing.example.json)
and set `metrics_url` to the full endpoint URL reachable **from Prometheus**.
For a bridged container, this may be a host bridge address or an HTTPS hostname.
The example's `host.docker.internal` is configured through Docker's host gateway;
Stacklab must actually listen on an address reachable through that gateway.
A loopback-only listener is unsuitable for this bridged mode.

Existing mode requires services named `prometheus` and `grafana`, Prometheus
attached to the configured Compose `network`, and existing file provisioning in
Grafana. `prometheus_config` must be mounted at `/etc/prometheus/prometheus.yml`,
and `dashboard_dir` must point to the host directory already read by a Grafana
provider. The defaults match Stacklab's `config/<stack>/prometheus/` and
`config/<stack>/grafana/dashboards/` layout.

The tool merges three scrape jobs and two exporters into the existing files. It
preserves other services, jobs, alert rules, retention, Grafana settings, networks,
and named data volumes. YAML formatting/comments can change when files are
serialized; review the Git diff. Existing provider and datasource configuration
is kept; select your Prometheus datasource in the dashboard if its UID differs.

On first adoption of an integration configured manually, existing `node-exporter`,
`cadvisor` services or monitoring jobs cause a conflict. Review those definitions,
then use `--adopt-existing` with the preview and first apply. Subsequent runs
recognize the `x-stacklab-monitoring` marker committed in the Compose file, so
adoption is not needed after cloning that configuration to another host.

### Configuration Options

Only the setup JSON contains installation-specific choices. Unknown keys are
rejected to catch misspellings. Tokens, passwords and package versions do not
belong in this file.

| Option | Default / purpose |
| --- | --- |
| `schema_version` | `1` |
| `mode` | `standalone` or `existing` |
| `workspace`, `state_dir` | `/srv/stacklab`, `/var/lib/stacklab`; state must be outside the Git workspace |
| `stack`, `host_label` | Compose project `monitoring`, displayed host `server` |
| `metrics_url` | `http://127.0.0.1:8080/metrics`; full URL without embedded credentials |
| `systemd_unit`, `service_user` | `stacklab.service`, `stacklab`; must match the installed unit |
| `prometheus_gid` | `65534`; numeric group that can read the Prometheus token bind mount |
| `compose_file` | `<workspace>/stacks/<stack>/compose.yaml` |
| `prometheus_config` | `<workspace>/config/<stack>/prometheus/prometheus.yml` |
| `dashboard_dir` | `<workspace>/config/<stack>/grafana/dashboards` |
| `network` | `monitoring`; existing mode's exporter/Prometheus network |
| `host_gateway` | `auto`; detect the Docker bridge gateway in existing mode |
| `docker_root` | `auto`; detect Docker's data directory for cAdvisor |
| `grafana_listen` | `127.0.0.1:3000`; standalone Grafana listener |
| `prometheus_listen` | `127.0.0.1:9090`; standalone listener / existing Prometheus address inside its container |
| `node_exporter_port`, `cadvisor_port` | `9100`, `8081`; cAdvisor's port option applies to standalone mode |

### Reapply, Update, And Recover

The token lives at `<state_dir>/monitoring/metrics-token`. An existing token is
retained; a missing one is generated locally. Its mode is `0440`, owner is the
service user, and group is `prometheus_gid`. The tool writes a separate environment
file and a `90-monitoring.conf` systemd drop-in. It preserves the main Stacklab
environment file and restarts Stacklab only when its metrics settings change.
Reapplying matching configuration preserves the token inode and avoids a service
restart. A package upgrade does not remove these runtime settings.

Run the same command after changing setup options or adopting new exporter or
dashboard templates from a Stacklab release. Prometheus/Grafana image choices in
an existing stack remain under your control. `--deploy` pulls missing images,
validates Prometheus configuration, recreates affected containers to attach updated
files, and waits for all three targets plus application metrics. To verify later:

```bash
python3 /usr/lib/stacklab/monitoring/setup.py --config /srv/stacklab/monitoring-setup.json --verify
```

Verification requires Docker access but does not read or print the token. It
queries Prometheus from inside its container, including when hostnames only
resolve in Docker.

Before writing changed files, the tool stores their prior contents and metadata
under `<state_dir>/monitoring/backups/<timestamp>/`, together with a `restore.json`
mapping. On failure it restores those files and the prior metrics settings. If
container deployment already started, reconcile the running stack with the
restored Compose configuration; on a first deployment, remove only the newly
created monitoring containers. Data volumes are never deleted by the tool.
Keep the backup directory private because it may contain old credentials.

### Move To Another Server

1. Commit the setup JSON, generated Compose file and monitoring configuration
   directories to your workspace repository. Review the diff before pushing.
2. Install Stacklab, Docker and the setup tool dependencies on the new host; clone
   the workspace. Update `workspace`, addresses and `host_label` if they changed.
   In existing mode also adapt the existing stack's own bind paths/networks.
3. Restore Stacklab's private state if migrating the whole application. For
   monitoring alone, either restore the token privately or let setup generate a
   new one; the new Prometheus instance will use the same token file automatically.
4. Run the same `--apply --deploy` command. It recreates the host settings from the
   versioned configuration; no installer prepared for the old host is needed.
5. Restore Prometheus/Grafana named volumes from a consistent backup if you need
   prior history or other saved Grafana state. Without those volumes, provisioning
   recreates the dashboard and datasource, and metric history starts fresh.

The setup state receipt and backup directory are not needed to reproduce the
configuration from Git. Tokens and measurement history are intentionally separate
from the versioned files.

## Manual Setup Reference

The remaining sections describe the individual steps for installations where the
operator manages host configuration through another provisioning system.

### Enable Stacklab Metrics

Use a build with the `/metrics` endpoint. On a package-managed Linux host:

```bash
sudo install -d -o stacklab -g stacklab -m 0750 /var/lib/stacklab/monitoring
sudo sh -c 'umask 077; openssl rand -hex 32 > /var/lib/stacklab/monitoring/metrics-token'
sudo chown stacklab:65534 /var/lib/stacklab/monitoring/metrics-token
sudo chmod 0440 /var/lib/stacklab/monitoring/metrics-token
```

The file owner permits Stacklab to read it; group 65534 permits the Prometheus
container's `nobody` user to read the bind-mounted file. Adjust the group if your
Prometheus container uses a different identity. Keep this file outside
`/srv/stacklab` and Git. Do not print it, put it in a scrape URL, or paste it into
the Grafana dashboard.

Add to `/etc/stacklab/stacklab.env`, then restart Stacklab:

```text
STACKLAB_METRICS_TOKEN_FILE=/var/lib/stacklab/monitoring/metrics-token
```

`/metrics` uses the main `STACKLAB_HTTP_ADDR` listener. Choose a scrape target
reachable from Prometheus: a host Docker-bridge address on an internal network,
or an HTTPS reverse-proxy address. A loopback-only listener cannot be reached
through `host.docker.internal` from a bridged container. Do not broaden the
application's listener to all interfaces just to enable monitoring.

The token grants access only to `/metrics`. Browser sessions do not grant scrape
access. Changing the configured token requires a Stacklab restart and recreation
of Prometheus if a single-file bind mount was replaced atomically.

See the [metrics contract](../api/service-metrics.md) for names and semantics.

## Add The Exporters And Scrape Jobs

[compose.exporters.yaml](../../deploy/monitoring/compose.exporters.yaml) is an
override for an existing Compose stack with a `prometheus` service and
`monitoring` network. Merge its services and the Prometheus secret mount into the
managed stack's main `compose.yaml`, or pass both files to Compose. Preserve the
existing project name, networks, and Prometheus/Grafana data volumes.

node_exporter uses the host PID/network namespaces and a read-only root mount so
it reports host interfaces and filesystems. Its listener binds only to Docker's
host gateway, normally `172.17.0.1`; set `MONITORING_HOST_GATEWAY` if it differs.
Its enabled collectors match the dashboard, avoiding unrelated storage/network
subsystem collectors. Virtual bridge/veth interfaces are excluded.
cAdvisor needs privileged cgroup/device access for Docker on Linux. It has no
published port and exports only the whitelisted Compose project/service labels,
not arbitrary container labels or environment variables. Both exporters are
excluded from Traefik routing.

Adapt [prometheus.scrape.yaml](../../deploy/monitoring/prometheus.scrape.yaml) to
your listener address and append the three jobs to your existing `scrape_configs`.
Keep the same `host` value on all three jobs. Retain the existing scrape interval
and retention policy; 15-second collection and 30-day retention work well for
this dashboard. Pull only the new exporter images when adding monitoring.

Validate the combined Compose and Prometheus configurations before deployment:

```bash
docker compose config --quiet
docker compose exec -T prometheus promtool check config /etc/prometheus/prometheus.yml
docker compose up -d --pull never prometheus node-exporter cadvisor
```

The first deployment recreates Prometheus to attach the secret mount; its named
data volume is preserved. Later scrape configuration edits can use
`docker compose kill -s SIGHUP prometheus` after `promtool check config` passes.

## Provision And Verify The Dashboard

Place `stacklab-overview.json` under the existing provisioned dashboard directory,
for example `grafana/dashboards/Homelab/stacklab-overview.json`. A provider with
`foldersFromFilesStructure: true` creates the folder automatically. Grafana picks
up the file at its configured polling interval; its default home dashboard can
remain unchanged.

Verify all three targets are `UP` in Prometheus, then open
`/d/stacklab-overview` in Grafana. The top row distinguishes successful collection
from Stacklab readiness. Rates need at least two samples; p95 and average job
duration can be empty when no requests/jobs occurred in the selected window.
Temperature panels require available hwmon sensors, and OOM series depend on
cAdvisor/kernel support. Container CPU uses percent of one core, so a container
using multiple cores can exceed 100%. Working-set memory differs from process
RSS. HTTP p95 includes WebSocket handlers when they close.
Containers using host networking share host network counters; their throughput
cannot be attributed exclusively to one container.

Source and credential changes should be backed up before deployment. To roll
back, restore the previous Compose/scrape configuration, remove only the two
exporter services, restore the prior dashboard file if present, and recreate
Prometheus with its original named volume. Never use `docker compose down -v`.
Disable Stacklab scraping by removing its token-file setting and restarting the
service. Filesystem metadata and the token should retain their original ownership
and permissions when restored.

The dashboard JSON is generated by
[generate-dashboard.py](../../scripts/monitoring/generate-dashboard.py), which
uses only Python's standard library. Regenerate it after editing panel definitions.
