#!/usr/bin/env python3
"""Generate the portable Stacklab Grafana dashboard using only the standard library."""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
DATASOURCE = {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}
HOST = 'job="node-exporter",host="$host"'
APP = 'job="stacklab",host="$host"'
CONTAINER = 'job="cadvisor",host="$host",image!="",container_label_com_docker_compose_project=~"$stack"'
panels = []
y = 0


def row(title):
    global y
    panels.append({"id": len(panels) + 1, "type": "row", "title": title,
                   "collapsed": False, "panels": [], "gridPos": {"x": 0, "y": y, "w": 24, "h": 1}})
    y += 1


def panel(title, queries, *, unit="short", kind="timeseries", x=0, width=12, height=8,
          description="", thresholds=None, maximum=None, mappings=None):
    targets = [{"refId": chr(65 + index), "expr": expr, "legendFormat": legend,
                "range": kind == "timeseries", "instant": kind != "timeseries",
                "datasource": DATASOURCE}
               for index, (expr, legend) in enumerate(queries)]
    defaults = {"unit": unit, "color": {"mode": "palette-classic"}, "decimals": 1,
                "thresholds": {"mode": "absolute", "steps": thresholds or [{"color": "green", "value": None}]},
                "mappings": mappings or []}
    if maximum is not None:
        defaults.update({"min": 0, "max": maximum})
    options = {"tooltip": {"mode": "multi", "sort": "desc"},
               "legend": {"displayMode": "table", "placement": "bottom", "calcs": ["lastNotNull", "max"]}}
    if kind == "timeseries":
        defaults["custom"] = {"drawStyle": "line", "lineInterpolation": "linear", "lineWidth": 1,
                              "fillOpacity": 12, "showPoints": "never", "spanNulls": False,
                              "axisLabel": "", "axisPlacement": "auto", "stacking": {"mode": "none", "group": "A"}}
    else:
        defaults["color"] = {"mode": "thresholds"}
        options = {"reduceOptions": {"calcs": ["lastNotNull"], "fields": "", "values": False},
                   "orientation": "auto", "textMode": "auto", "colorMode": "value", "graphMode": "none",
                   "justifyMode": "auto"}
    panels.append({"id": len(panels) + 1, "type": kind, "title": title, "description": description,
                   "datasource": DATASOURCE, "targets": targets,
                   "gridPos": {"x": x, "y": y, "w": width, "h": height},
                   "fieldConfig": {"defaults": defaults, "overrides": []}, "options": options})


status_mapping = [{"type": "value", "options": {"0": {"text": "DOWN", "color": "red"},
                                                  "1": {"text": "UP", "color": "green"}}}]
ready_mapping = [{"type": "value", "options": {"0": {"text": "NOT READY", "color": "red"},
                                                 "1": {"text": "READY", "color": "green"}}}]
status_thresholds = [{"color": "red", "value": None}, {"color": "green", "value": 1}]

row("Collection & application health")
for index, (title, job) in enumerate([("Host exporter", "node-exporter"), ("Docker exporter", "cadvisor"), ("Stacklab scrape", "stacklab")]):
    panel(title, [(f'up{{job="{job}",host="$host"}}', "")], kind="stat", x=index * 4, width=4, height=4,
          mappings=status_mapping, thresholds=status_thresholds,
          description="UP means Prometheus collected the target successfully. An empty panel means this target has no samples.")
panel("Stacklab readiness", [(f"stacklab_ready{{{APP}}}", "")], kind="stat", x=12, width=4, height=4,
      mappings=ready_mapping, thresholds=status_thresholds, description="Database, frontend, and runtime checks must all pass.")
panel("Host uptime", [(f"time() - node_boot_time_seconds{{{HOST}}}", "")], kind="stat", unit="s", x=16, width=4, height=4)
panel("Stacklab uptime", [(f"stacklab_uptime_seconds{{{APP}}}", "")], kind="stat", unit="s", x=20, width=4, height=4)
y += 4

row("Host — capacity & pressure")
panel("CPU utilization", [(f'100 * (1 - avg by (host) (rate(node_cpu_seconds_total{{{HOST},mode="idle"}}[$__rate_interval])))', "CPU")], unit="percent", maximum=100)
panel("Memory utilization", [(f"100 * (1 - node_memory_MemAvailable_bytes{{{HOST}}} / node_memory_MemTotal_bytes{{{HOST}}})", "RAM")], unit="percent", maximum=100, x=12)
y += 8
panel("Load average & CPU cores", [(f"node_load{window}{{{HOST}}}", f"Load {window}m") for window in [1, 5, 15]] +
      [(f'count by (host) (node_cpu_seconds_total{{{HOST},mode="idle"}})', "CPU cores")])
panel("Memory & swap", [(f"node_memory_MemTotal_bytes{{{HOST}}} - node_memory_MemAvailable_bytes{{{HOST}}}", "RAM used"),
                        (f"node_memory_MemAvailable_bytes{{{HOST}}}", "RAM available"),
                        (f"node_memory_SwapTotal_bytes{{{HOST}}} - node_memory_SwapFree_bytes{{{HOST}}}", "Swap used")], unit="bytes", x=12)
y += 8
filesystem = HOST + ',fstype!~"tmpfs|devtmpfs|overlay|squashfs|proc|sysfs|nsfs|ramfs",mountpoint!~"/run.*|/var/lib/docker.*"'
panel("Filesystem space used", [(f"100 * (1 - node_filesystem_avail_bytes{{{filesystem}}} / node_filesystem_size_bytes{{{filesystem}}})", "{{mountpoint}}")], unit="percent", maximum=100,
      description="Uses space available to non-root processes. Virtual and Docker overlay mounts are excluded.")
panel("Filesystem inodes used", [(f"100 * (1 - node_filesystem_files_free{{{filesystem}}} / node_filesystem_files{{{filesystem}}})", "{{mountpoint}}")], unit="percent", maximum=100, x=12)
y += 8
disk = HOST + ',device!~"loop.*|ram.*|fd.*|sr.*"'
panel("Disk throughput", [(f"rate(node_disk_read_bytes_total{{{disk}}}[$__rate_interval])", "{{device}} read"),
                          (f"rate(node_disk_written_bytes_total{{{disk}}}[$__rate_interval])", "{{device}} write")], unit="Bps")
network = HOST + ',device!~"lo|veth.*|docker.*|br-.*"'
panel("Host network throughput", [(f"rate(node_network_receive_bytes_total{{{network}}}[$__rate_interval])", "{{device}} receive"),
                                  (f"rate(node_network_transmit_bytes_total{{{network}}}[$__rate_interval])", "{{device}} transmit")], unit="Bps", x=12)
y += 8
panel("Disk busy time", [(f"100 * rate(node_disk_io_time_seconds_total{{{disk}}}[$__rate_interval])", "{{device}}")], unit="percent")
panel("Hardware temperatures", [(f"node_hwmon_temp_celsius{{{HOST}}}", "{{chip}} / {{sensor}}")], unit="celsius", x=12,
      description="Available hwmon sensors; an empty panel means the host does not expose supported sensors.")
y += 8

row("Docker — Compose project filter")
panel("Containers reporting recently", [(f"count(container_last_seen{{{CONTAINER}}} > time() - 60)", "Containers")], kind="stat", width=8, height=4,
      description="Containers seen by cAdvisor in the last minute. This is not a Docker healthcheck result.")
panel("Container working-set memory", [(f"sum(container_memory_working_set_bytes{{{CONTAINER}}})", "Working set")], kind="stat", x=8, width=8, height=4, unit="bytes")
panel("OOM events in selected range", [(f"sum(increase(container_oom_events_total{{{CONTAINER}}}[$__range]))", "OOM events")], kind="stat", x=16, width=8, height=4,
      description="Requires cAdvisor/kernel OOM event support; missing data is not the same as zero events.",
      thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}])
y += 4
legend = "{{container_label_com_docker_compose_project}} / {{name}}"
panel("CPU per container", [(f"100 * rate(container_cpu_usage_seconds_total{{{CONTAINER}}}[$__rate_interval])", legend)], unit="percent",
      description="100% equals one fully used CPU core; multicore containers can exceed 100%.")
panel("Working-set memory per container", [(f"container_memory_working_set_bytes{{{CONTAINER}}}", legend)], unit="bytes", x=12)
y += 8
container_network = CONTAINER + ',interface!~"lo|veth.*|docker.*|br-.*"'
panel("Container network receive", [(f"sum by (name,container_label_com_docker_compose_project) (rate(container_network_receive_bytes_total{{{container_network}}}[$__rate_interval]))", legend)], unit="Bps",
      description="Containers using host networking share host counters; traffic is not exclusive to each container.")
panel("Container network transmit", [(f"sum by (name,container_label_com_docker_compose_project) (rate(container_network_transmit_bytes_total{{{container_network}}}[$__rate_interval]))", legend)], unit="Bps", x=12,
      description="Containers using host networking share host counters; traffic is not exclusive to each container.")
y += 8
panel("CPU throttled periods", [(f"100 * rate(container_cpu_cfs_throttled_periods_total{{{CONTAINER}}}[$__rate_interval]) / rate(container_cpu_cfs_periods_total{{{CONTAINER}}}[$__rate_interval])", legend)], unit="percent", maximum=100,
      description="Share of CFS periods throttled; meaningful when the container has a CPU quota.")
panel("Container uptime", [(f"time() - container_start_time_seconds{{{CONTAINER}}}", legend)], unit="s", x=12)
y += 8

row("Stacklab — traffic, jobs & process")
panel("HTTP requests & service errors", [(f"rate(stacklab_http_requests_total{{{APP}}}[$__rate_interval])", "Requests / s"),
                                       (f"rate(stacklab_http_errors_total{{{APP}}}[$__rate_interval])", "5xx / s")], unit="reqps",
      description="Prometheus scrapes are excluded. Client 4xx responses are not service errors.")
panel("HTTP duration — p95 & mean", [(f"histogram_quantile(0.95, sum by (le) (rate(stacklab_http_request_duration_seconds_bucket{{{APP}}}[$__rate_interval])))", "p95"),
                                    (f"rate(stacklab_http_request_duration_seconds_sum{{{APP}}}[$__rate_interval]) / rate(stacklab_http_request_duration_seconds_count{{{APP}}}[$__rate_interval])", "Mean")], unit="s", x=12,
      description="Completed HTTP handlers, including WebSockets when they close. Empty when no requests completed in the window.")
y += 8
panel("Jobs — active, completed & failed", [(f"stacklab_jobs_active{{{APP}}}", "Active"),
                                         (f"increase(stacklab_jobs_completed_total{{{APP}}}[5m])", "Completed / 5m"),
                                         (f"increase(stacklab_jobs_errors_total{{{APP}}}[5m])", "Failed or timed out / 5m")])
panel("Job duration — p95 & mean", [(f"histogram_quantile(0.95, sum by (le) (rate(stacklab_job_duration_seconds_bucket{{{APP}}}[$__rate_interval])))", "p95"),
                                   (f"rate(stacklab_job_duration_seconds_sum{{{APP}}}[$__rate_interval]) / rate(stacklab_job_duration_seconds_count{{{APP}}}[$__rate_interval])", "Mean")], unit="s", x=12,
      description="Includes succeeded, failed, timed out and cancelled jobs. Empty while no jobs complete in the window.")
y += 8
panel("HTTP handlers & WebSocket connections", [(f"stacklab_http_requests_in_flight{{{APP}}}", "HTTP handlers (includes WS)"),
                                               (f"stacklab_websocket_connections_active{{{APP}}}", "WebSockets"),
                                               (f"increase(stacklab_websocket_errors_total{{{APP}}}[5m])", "WS errors / 5m")])
panel("Stacklab readiness checks", [(f"stacklab_readiness_check{{{APP}}}", "{{component}}")], maximum=1, x=12)
y += 8
panel("Stacklab process CPU", [(f"100 * rate(process_cpu_seconds_total{{{APP}}}[$__rate_interval])", "Process CPU")], unit="percent",
      description="Percent of one CPU core; this is the native Stacklab process, not the exporter process.")
panel("Stacklab memory — RSS & Go heap", [(f"process_resident_memory_bytes{{{APP}}}", "Process RSS"),
                                        (f"go_memstats_heap_alloc_bytes{{{APP}}}", "Go heap allocated"),
                                        (f"go_memstats_heap_inuse_bytes{{{APP}}}", "Go heap in use")], unit="bytes", x=12)
y += 8
panel("Go goroutines & open file descriptors", [(f"go_goroutines{{{APP}}}", "Goroutines"), (f"process_open_fds{{{APP}}}", "Open file descriptors")])
panel("Go GC pause time", [(f"rate(go_gc_duration_seconds_sum{{{APP}}}[$__rate_interval])", "GC seconds / second")], unit="s", x=12)
y += 8

variables = [
    {"name": "DS_PROMETHEUS", "label": "Datasource", "type": "datasource", "query": "prometheus",
     "current": {"text": "Prometheus", "value": "prometheus"}, "refresh": 1},
    {"name": "host", "label": "Host", "type": "query", "datasource": DATASOURCE,
     "query": 'label_values(up{job="node-exporter"}, host)', "refresh": 1, "sort": 1,
     "current": {"text": "homelab", "value": "homelab"}},
    {"name": "stack", "label": "Compose project", "type": "query", "datasource": DATASOURCE,
     "query": 'label_values(container_last_seen{job="cadvisor",host="$host",container_label_com_docker_compose_project!=""}, container_label_com_docker_compose_project)',
     "refresh": 1, "sort": 1, "multi": False, "includeAll": True, "allValue": ".*",
     "current": {"text": "All", "value": "$__all"}},
]

dashboard = {"id": None, "uid": "stacklab-overview", "title": "Stacklab — Host & Docker",
             "description": "Host, Docker containers, and the native Stacklab service. Select a host and optionally a Compose project.",
             "tags": ["stacklab", "homelab", "docker"], "timezone": "browser", "schemaVersion": 39,
             "version": 1, "editable": False, "refresh": "15s", "time": {"from": "now-6h", "to": "now"},
             "timepicker": {"refresh_intervals": ["15s", "30s", "1m", "5m"]},
             "templating": {"list": variables}, "annotations": {"list": []}, "panels": panels}

if __name__ == "__main__":
    destination = ROOT / "deploy/monitoring/stacklab-overview.json"
    destination.write_text(json.dumps(dashboard, indent=2, ensure_ascii=False) + "\n")
    print(f"Generated {destination.relative_to(ROOT)} ({len(panels)} panels including section rows)")
