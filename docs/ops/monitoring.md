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

## Enable Stacklab Metrics

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
