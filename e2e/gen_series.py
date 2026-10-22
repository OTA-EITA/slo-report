#!/usr/bin/env python3
"""Print 6 hours of synthetic request counters in Prometheus text format with
timestamps, for /api/v1/import/prometheus.

checkout fails 2% of requests (burns the 99.9% budget 20x too fast);
search fails 0.01% (well within budget); billing has no data at all.
"""
import time

now_ms = int(time.time()) * 1000
step_ms = 60_000
points = 6 * 60

services = {
    # job: (ok requests per minute, failed requests per minute)
    "checkout": (980, 20),
    "search": (9999, 1),
}

for i in range(points + 1):
    ts = now_ms - (points - i) * step_ms
    for job, (ok, failed) in services.items():
        print(f'http_requests_total{{job="{job}",code="200"}} {ok * i} {ts}')
        print(f'http_requests_total{{job="{job}",code="500"}} {failed * i} {ts}')
