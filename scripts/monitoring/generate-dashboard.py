#!/usr/bin/env python3
"""Generate the portable Stacklab Grafana dashboard using only the standard library."""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
DATASOURCE = {"type": "prometheus", "uid": "${DS_PROMETHEUS}"}
HOST = 'job="node-exporter",host="$host"'
APP = 'job="stacklab",host="$host"'
CONTAINER = 'job="cadvisor",host="$host",image!="",container_label_com_docker_compose_project=~"$stack"'
CONTAINER_GROUP = "host,container_label_com_docker_compose_project,name"
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
                "range": kind in {"timeseries", "state-timeline"}, "instant": kind not in {"timeseries", "state-timeline"},
                "datasource": DATASOURCE}
               for index, (expr, legend) in enumerate(queries)]
    defaults = {"unit": unit, "color": {"mode": "palette-classic"}, "decimals": 1,
                "thresholds": {"mode": "absolute", "steps": thresholds or [{"color": "green", "value": None}]},
                "mappings": mappings or []}
    if maximum is not None:
        defaults.update({"min": 0, "max": maximum})
    options = {"tooltip": {"mode": "multi", "sort": "desc"},
               "legend": {"displayMode": "table", "placement": "bottom", "calcs": ["last", "max"]}}
    if kind == "timeseries":
        defaults["custom"] = {"drawStyle": "line", "lineInterpolation": "linear", "lineWidth": 1,
                              "fillOpacity": 12, "showPoints": "never", "spanNulls": False,
                              "axisLabel": "", "axisPlacement": "auto", "stacking": {"mode": "none", "group": "A"}}
    elif kind == "state-timeline":
        defaults.update(color={"mode": "thresholds"},
                        custom={"lineWidth": 0, "fillOpacity": 85, "spanNulls": False},
                        thresholds={"mode": "absolute", "steps": [{"color": "orange", "value": None}]},
                        mappings=[{"type": "value", "options": {"1": {"text": "Firing", "color": "orange"}}}])
        options = {"mergeValues": True, "showValue": "never", "rowHeight": 0.8,
                   "alignValue": "center", "legend": {"showLegend": False},
                   "tooltip": {"mode": "single", "sort": "none"}}
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
alerts_y = y
y += 4

row("Host — capacity & pressure")
filesystem_summary_y = y
y += 4
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
      thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 85}, {"color": "red", "value": 95}],
      description="Uses space available to non-root processes. Virtual and Docker overlay mounts are excluded. Warning thresholds follow the monitoring setup's per-filesystem policy; critical is 95% used.")
panels[-1]["fieldConfig"]["defaults"]["custom"]["thresholdsStyle"] = {"mode": "line"}
panel("Filesystem inodes used", [(f"100 * (1 - node_filesystem_files_free{{{filesystem}}} / (node_filesystem_files{{{filesystem}}} > 0))", "{{mountpoint}}")], unit="percent", maximum=100, x=12,
      description="Filesystems without a finite inode count (for example Btrfs and FAT) are omitted.")
y += 8
disk = HOST + ',device!~"loop.*|ram.*|fd.*|sr.*"'
panel("Disk throughput", [(f"rate(node_disk_read_bytes_total{{{disk}}}[$__rate_interval])", "{{device}} read"),
                          (f"rate(node_disk_written_bytes_total{{{disk}}}[$__rate_interval])", "{{device}} write")], unit="Bps")
network = HOST + ',device!~"lo|veth.*|docker.*|br-.*"'
panel("Host network throughput", [(f"rate(node_network_receive_bytes_total{{{network}}}[$__rate_interval])", "{{device}} receive"),
                                  (f"rate(node_network_transmit_bytes_total{{{network}}}[$__rate_interval])", "{{device}} transmit")], unit="Bps", x=12)
y += 8
panel("Disk busy time", [(f"100 * rate(node_disk_io_time_seconds_total{{{disk}}}[$__rate_interval])", "{{device}}")], unit="percent")
panel("Hardware temperatures", [(f"node_hwmon_temp_celsius{{{HOST}}} >= 0 <= 150", "{{chip}} / {{sensor}}")], unit="celsius", x=12,
      description="Host hardware sensors within 0–150°C. Invalid firmware readings (such as −263.2°C) are omitted, not converted to zero. Empty means no usable samples.")
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
panel("CPU per container", [(f"100 * sum by ({CONTAINER_GROUP}) (rate(container_cpu_usage_seconds_total{{{CONTAINER}}}[$__rate_interval]))", legend)], unit="percent",
      description="100% equals one fully used CPU core; multicore containers can exceed 100%. Recreated containers are grouped by host, Compose project and container name.")
panel("Working-set memory per container", [(f"sum by ({CONTAINER_GROUP}) (container_memory_working_set_bytes{{{CONTAINER}}})", legend)], unit="bytes", x=12,
      description="Recreated containers are grouped by host, Compose project and container name.")
y += 8
network_notice_y = y
y += 4
container_network = CONTAINER + ',interface!~"lo|veth.*|docker.*|br-.*"'
panel("Network receive seen by container", [(f"sum by (name,container_label_com_docker_compose_project) (rate(container_network_receive_bytes_total{{{container_network}}}[$__rate_interval]))", legend)], unit="Bps",
      description="Containers using host networking share host counters; traffic is not exclusive to each container.")
panel("Network transmit seen by container", [(f"sum by (name,container_label_com_docker_compose_project) (rate(container_network_transmit_bytes_total{{{container_network}}}[$__rate_interval]))", legend)], unit="Bps", x=12,
      description="Containers using host networking share host counters; traffic is not exclusive to each container.")
y += 8
panel("CPU throttled periods", [(f"100 * sum by ({CONTAINER_GROUP}) (rate(container_cpu_cfs_throttled_periods_total{{{CONTAINER}}}[$__rate_interval])) / sum by ({CONTAINER_GROUP}) (rate(container_cpu_cfs_periods_total{{{CONTAINER}}}[$__rate_interval]))", legend)], unit="percent", maximum=100,
      description="Share of CFS periods throttled. No data is expected without CPU quotas or when cAdvisor does not expose CFS counters; it does not establish zero throttling.")
panels[-1]["fieldConfig"]["defaults"]["noValue"] = "CFS counters unavailable"
panel("Container uptime", [(f"time() - max by ({CONTAINER_GROUP}) (container_start_time_seconds{{{CONTAINER}}})", legend)], unit="s", x=12,
      description="Age of the newest instance for each host, Compose project and container name. A recreation resets the line instead of creating a duplicate legend entry.")
y += 8

row("Stacklab — traffic, jobs & process")
application_summary_y = y
y += 4
panel("HTTP requests & service errors", [(f"rate(stacklab_http_requests_total{{{APP}}}[$__rate_interval])", "Requests / s"),
                                       (f"rate(stacklab_http_errors_total{{{APP}}}[$__rate_interval])", "5xx / s")], unit="reqps",
      description="Prometheus scrapes are excluded. Client 4xx responses are not service errors.")
panel("HTTP response duration — p95 & mean", [(f"histogram_quantile(0.95, sum by (le) (rate(stacklab_http_response_duration_seconds_bucket{{{APP}}}[$__rate_interval])))", "p95"),
                                    (f"rate(stacklab_http_response_duration_seconds_sum{{{APP}}}[$__rate_interval]) / rate(stacklab_http_response_duration_seconds_count{{{APP}}}[$__rate_interval])", "Mean")], unit="s", x=12,
      description="Completed HTTP responses, excluding upgraded WebSocket connections and Prometheus scrapes. Empty when no responses complete, or before the response histogram was introduced; no fallback to WebSocket handler lifetimes.")
y += 8
panel("Jobs — active, completed & failed", [(f"stacklab_jobs_active{{{APP}}}", "Active"),
                                         (f"increase(stacklab_jobs_completed_total{{{APP}}}[$__rate_interval])", "Completed / window"),
                                         (f"increase(stacklab_jobs_errors_total{{{APP}}}[$__rate_interval])", "Failed or timed out / window")],
      description="Active jobs are instantaneous. Event counts use an overlapping window that grows with the time range, so events between graph points remain visible. Windows overlap; use the selected-range totals instead of summing graph points.")
panel("Job duration — p95 & mean", [(f"histogram_quantile(0.95, sum by (le) (rate(stacklab_job_duration_seconds_bucket{{{APP}}}[$__rate_interval])))", "p95"),
                                   (f"rate(stacklab_job_duration_seconds_sum{{{APP}}}[$__rate_interval]) / rate(stacklab_job_duration_seconds_count{{{APP}}}[$__rate_interval])", "Mean")], unit="s", x=12,
      description="Includes succeeded, failed, timed out and cancelled jobs. Empty while no jobs complete in the window.")
y += 8
panel("HTTP handlers & WebSocket connections", [(f"stacklab_http_requests_in_flight{{{APP}}}", "HTTP handlers (includes WS)"),
                                               (f"stacklab_websocket_connections_active{{{APP}}}", "WebSockets"),
                                               (f"increase(stacklab_websocket_errors_total{{{APP}}}[$__rate_interval])", "WS errors / window")],
      description="Connection counts are instantaneous. WebSocket errors use an overlapping window that grows with the time range.")
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
extra_y = y

# Append new panels to preserve the IDs of existing panels and saved links.
y = alerts_y
alert_selector = 'stacklab_monitoring="true",host="$host",alertstate="firing"'
panel("Active infrastructure alerts", [(f'sum(ALERTS{{{alert_selector}}}) or (0 * max(stacklab_monitoring_expected_target{{host="$host"}}))', "Alerts")],
      kind="stat", width=6, height=4,
      thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}],
      description="All infrastructure alerts for this host, regardless of Compose project selection. No data means the bundled rules are not loaded or Prometheus is unavailable. Notifications require an Alertmanager receiver.")
panel("Infrastructure alert history", [(f'max_over_time(ALERTS{{{alert_selector}}}[$__rate_interval])', "{{alertname}} · {{job}} · {{mountpoint}} {{device}} {{name}}")],
      kind="state-timeline", x=6, width=18, height=4, maximum=1,
      description="Colored intervals contain a firing evaluation within the sampling window. The window grows with the time range so brief alerts remain visible; it can extend the displayed interval. Gaps alone do not prove health. See Active infrastructure alerts for the current state.")
panels.append({"id": len(panels) + 1, "type": "text", "title": "Reading container network traffic",
               "gridPos": {"x": 0, "y": network_notice_y, "w": 24, "h": 4},
               "options": {"mode": "markdown", "content": "These counters belong to the network namespace visible to each container. **Containers using host networking see shared host traffic; do not add their series or attribute that traffic to an individual application.** Use **Host network throughput** for host totals. Containers sharing another container’s network namespace have the same limitation."}})

y = extra_y
panel("WebSocket failures by reason", [(f"increase(stacklab_websocket_failures_total{{{APP}}}[$__rate_interval])", "{{operation}} / {{reason}}")],
      description="Unexpected failures per overlapping sampling window, at most one per connection. The window grows with the time range. Normal closes, navigation, revoked sessions and intentional server shutdown are excluded. Timeouts and abrupt disconnects remain visible.")
panel("Closed WebSocket connection duration — mean", [(f"rate(stacklab_websocket_connection_duration_seconds_sum{{{APP}}}[$__rate_interval]) / rate(stacklab_websocket_connection_duration_seconds_count{{{APP}}}[$__rate_interval])", "Mean connection lifetime")],
      unit="s", x=12, description="Connection lifetime, separate from HTTP response latency. Empty when no connections closed in the window or before this histogram was introduced.")
y += 8
application_alerts = 'ALERTS{stacklab_monitoring!="true",host=~"$host|",alertstate="firing"}'
panel("Other application alert history", [(f'max_over_time({application_alerts}[$__rate_interval])', "{{alertname}} · {{job}} · {{route}} {{type}} {{instance}}")],
      kind="state-timeline", x=6, width=18, maximum=1,
      description="Colored intervals contain a firing evaluation within the adaptive sampling window; the interval can extend beyond the original alert. Route and type distinguish application alerts. Includes the selected host and alerts without a host label, which may belong to another host. See the separate count for the current state.")
panel("Active other application alerts", [(f'sum({application_alerts}) or (0 * max(stacklab_monitoring_expected_target{{host="$host"}}))', "Alerts")],
      kind="stat", width=6, height=8, thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}],
      description="Current non-infrastructure alerts for this host, plus alerts with no host label. Unassigned alerts may belong to another host. No data means the monitoring baseline is unavailable.")

y += 8
row("Hardware reliability — PCIe, NVMe & Btrfs")
fresh = f'(time() - stacklab_hardware_collection_timestamp_seconds{{{HOST}}} < 180)'
panel("Hardware collection status", [(f'stacklab_hardware_collector_success{{{HOST}}} and on (host, instance) {fresh}', "{{collector}}")],
      kind="stat", width=8, height=4, mappings=status_mapping, thresholds=status_thresholds,
      description="Read-only host collection runs every minute. UP means collection succeeded; hardware findings have separate panels. Empty means not enabled, unavailable or stale, not healthy.")
panel("Hardware data age", [(f'time() - stacklab_hardware_collection_timestamp_seconds{{{HOST}}}', "Age")],
      kind="stat", unit="s", x=8, width=8, height=4,
      thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 120}, {"color": "red", "value": 180}])
panel("NVMe SMART health", [(f'stacklab_nvme_health_passed{{{HOST}}} and on (host, instance) {fresh}', "{{device}}")],
      kind="stat", x=16, width=8, height=4, thresholds=status_thresholds,
      mappings=[{"type": "value", "options": {"0": {"text": "FAILED", "color": "red"}, "1": {"text": "PASSED", "color": "green"}}}],
      description="Controller SMART health from smartctl. Only explicitly configured NVMe controllers are queried. Missing/failed reads are not rendered as healthy.")
y += 4
panel("PCIe errors — new events / 15m", [(f'increase(stacklab_pcie_errors_total{{{HOST}}}[15m])', "{{device}} / {{severity}}")],
      description="AER counters per PCI device, sampled every minute. Correctable events were repaired by hardware; nonfatal/fatal are uncorrectable. Increases are estimates and handle counter resets. Root-port forwarded totals are not counted again.")
panel("PCIe correctable error types / 15m", [(f'increase(stacklab_pcie_correctable_errors_total{{{HOST}}}[15m])', "{{device}} / {{error}}")], x=12,
      description="RxErr indicates a corrected physical-link receive error. Zero differs from an unsupported AER counter; absence is not converted to zero.")
y += 8
panel("PCIe counters since device initialization", [(f'stacklab_pcie_errors_total{{{HOST}}}', "{{device}} / {{severity}}")],
      kind="stat", width=12, height=6, description="Historical counters, not a current alert. Reboot/device resets can clear them. Alerts use new events.")
panel("NVMe media & error-log counters", [(f'stacklab_nvme_media_errors_total{{{HOST}}}', "{{device}} media/data integrity"),
                                           (f'stacklab_nvme_error_log_entries_total{{{HOST}}}', "{{device}} error-log entries")], x=12, height=6,
      description="Lifetime counters. Error-log entries can include rejected/unsupported commands and do not alone prove media damage. New media errors trigger a critical alert.")
y += 6
panel("NVMe wear & spare capacity", [(f'stacklab_nvme_percentage_used{{{HOST}}}', "{{device}} endurance used"),
                                    (f'stacklab_nvme_available_spare_percent{{{HOST}}}', "{{device}} spare available"),
                                    (f'stacklab_nvme_available_spare_threshold_percent{{{HOST}}}', "{{device}} spare threshold")], unit="percent",
      description="Manufacturer endurance estimate, unrelated to filesystem free space; percentage used may exceed 100%. Alert at 90% used or spare below its device threshold.")
panel("NVMe temperature", [(f'stacklab_nvme_temperature_celsius{{{HOST}}}', "{{device}}")], unit="celsius", x=12)
y += 8
panel("NVMe critical-warning bitmask", [(f'stacklab_nvme_critical_warning{{{HOST}}} and on (host, instance) {fresh}', "{{device}}")],
      kind="stat", width=12, height=5, thresholds=[{"color": "green", "value": None}, {"color": "red", "value": 1}],
      description="Zero means no active controller warning. Any nonzero bitmask requires inspection with smartctl or nvme smart-log.")
panel("NVMe unsafe shutdowns — new events / 1h", [(f'increase(stacklab_nvme_unsafe_shutdowns_total{{{HOST}}}[1h])', "{{device}}")], x=12, height=5,
      description="Detects newly observed unsafe shutdowns; old lifetime totals alone do not trigger an alert. Events during a monitoring outage may not be observable as an increase.")
y += 5
panel("Btrfs errors — new events / 15m", [(f'increase(stacklab_btrfs_device_errors_total{{{HOST}}}[15m])', "{{filesystem}} / {{device_id}} / {{error}}")],
      description="Read/write/flush/corruption/generation errors from kernel sysfs. Counters are never reset by this collector. Empty on hosts without Btrfs or without supported error_stats.")
panel("Btrfs device error totals", [(f'stacklab_btrfs_device_errors_total{{{HOST}}}', "{{device_id}} / {{error}}"),
                                    (f'stacklab_btrfs_device_missing{{{HOST}}}', "{{device_id}} missing")], x=12,
      description="Persisted Btrfs device errors plus missing-device state. Inspect new-event alerts and btrfs device stats; existing totals can predate this installation.")
y += 8
row("Host reliability — memory, I/O & network")
panel("Host OOM kills / 15m", [(f'increase(node_vmstat_oom_kill{{{HOST}}}[15m])', "OOM kills")],
      description="Kernel OOM kills, including processes outside containers. Missing means the vmstat metric is unavailable.")
panel("Memory-controller ECC errors / 15m", [(f'increase(node_edac_correctable_errors_total{{{HOST}}}[15m])', "{{controller}} corrected"),
                                             (f'increase(node_edac_uncorrectable_errors_total{{{HOST}}}[15m])', "{{controller}} uncorrected")], x=12,
      description="Requires hardware/driver EDAC support. Empty on systems without exposed ECC counters; not proof of error-free RAM.")
y += 8
panel("Memory & I/O pressure", [(f'100 * rate(node_pressure_{resource}_{mode}_seconds_total{{{HOST}}}[$__rate_interval])', f'{resource} / {mode}')
                               for resource in ["memory", "io"] for mode in ["waiting", "stalled"]], unit="percent", maximum=100,
      description="Linux PSI: waiting means some tasks stalled; stalled means all non-idle tasks stalled. Distinguishes contention from simple memory usage or disk throughput.")
panel("Network errors & drops / window", [(f'increase(node_network_{direction}_{metric}_total{{{network}}}[$__rate_interval])', '{{device}} ' + direction + ' ' + metric)
                                    for direction in ["receive", "transmit"] for metric in ["errs", "drop"]], x=12,
      description="Host-interface events per overlapping window, growing with the time range so events between graph points remain visible. Alerts cover recurring errors; drops are separate because filtering and queue policy can intentionally drop packets.")
for hardware_panel in panels[47:]:
    if "fieldConfig" in hardware_panel:
        hardware_panel["fieldConfig"]["defaults"]["noValue"] = "Unavailable / not enabled"

# Fill reserved summary rows without changing existing panel IDs or saved links.
y = application_summary_y
for index, (title, expr, errors) in enumerate([
    ("Jobs completed — selected range", f'increase(stacklab_jobs_completed_total{{{APP}}}[$__range])', False),
    ("Jobs failed — selected range", f'increase(stacklab_jobs_errors_total{{{APP}}}[$__range])', True),
    ("HTTP service errors — selected range", f'increase(stacklab_http_errors_total{{{APP}}}[$__range])', True),
    ("WebSocket failures — selected range", f'sum(increase(stacklab_websocket_failures_total{{{APP}}}[$__range]))', True),
]):
    panel(title, [(expr, "Events")], kind="stat", x=index * 6, width=6, height=4,
          thresholds=[{"color": "green", "value": None}] + ([{"color": "red", "value": 1}] if errors else []),
          description="Estimated counter increase across the entire selected range, including counter resets. Missing data is not converted to zero.")
    panels[-1]["fieldConfig"]["defaults"]["decimals"] = 0
y = filesystem_summary_y
panel("Filesystem space used — current", [(f"100 * (1 - node_filesystem_avail_bytes{{{filesystem}}} / node_filesystem_size_bytes{{{filesystem}}})", "{{mountpoint}}")],
      kind="stat", width=12, height=4, unit="percent", maximum=100,
      thresholds=[{"color": "green", "value": None}, {"color": "yellow", "value": 85}, {"color": "red", "value": 95}],
      description="At the end of the selected range. Warning thresholds follow the monitoring setup's per-filesystem policy; critical is 95% used.")
panel("Filesystem space available — current", [(f"node_filesystem_avail_bytes{{{filesystem}}}", "{{mountpoint}}")],
      kind="stat", x=12, width=12, height=4, unit="bytes",
      description="Space available to non-root processes at the end of the selected range. See the utilization panel for capacity warnings.")

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
             "version": 1, "editable": False, "refresh": "1m", "time": {"from": "now-6h", "to": "now"},
             "timepicker": {"refresh_intervals": ["15s", "30s", "1m", "5m"]},
             "templating": {"list": variables}, "annotations": {"list": []}, "panels": panels}

if __name__ == "__main__":
    destination = ROOT / "deploy/monitoring/stacklab-overview.json"
    destination.write_text(json.dumps(dashboard, indent=2, ensure_ascii=False) + "\n")
    print(f"Generated {destination.relative_to(ROOT)} ({len(panels)} panels including section rows)")
