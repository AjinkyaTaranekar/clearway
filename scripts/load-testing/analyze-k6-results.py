#!/usr/bin/env python3
"""Analyze k6 load-test artifacts and produce per-API latency/error reports.

Reads the artifact directory produced by scripts/load-testing/run-load-test.sh
wrapper runs (manifest.csv + metrics/ + summaries/ + console/) and generates:
1) JSON report with per-run and aggregate per-API metrics
2) Markdown report for quick human-readable review

Metrics include:
- p50 / p95 / p99 latency per API
- per-API error rate
- API status code distributions
- failing thresholds and failing checks (from summary JSON)
- common console error messages

Note: response bodies are not recorded in k6 JSON output by default.
"""

from __future__ import annotations

import argparse
import csv
import datetime as dt
import json
import math
import re
from collections import Counter, defaultdict
from pathlib import Path
from typing import Any, Dict, List, Optional, Tuple
from urllib.parse import urlparse


ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")


class EndpointAccumulator:
    """Collect latency + status stats for one endpoint."""

    def __init__(self) -> None:
        self.durations_ms: List[float] = []
        self.status_counts: Counter[str] = Counter()
        self.error_count = 0
        self.total_count = 0

    def add(self, duration_ms: float, status: str) -> None:
        self.durations_ms.append(duration_ms)
        self.status_counts[status] += 1
        self.total_count += 1
        status_int = try_int(status)
        if status_int is not None and status_int >= 400:
            self.error_count += 1


def try_int(value: Any) -> Optional[int]:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def percentile(sorted_values: List[float], p: float) -> Optional[float]:
    if not sorted_values:
        return None
    if len(sorted_values) == 1:
        return sorted_values[0]
    k = (len(sorted_values) - 1) * (p / 100.0)
    f = math.floor(k)
    c = math.ceil(k)
    if f == c:
        return sorted_values[int(k)]
    return sorted_values[f] * (c - k) + sorted_values[c] * (k - f)


def fmt_ms(value: Optional[float]) -> str:
    if value is None:
        return "-"
    return f"{value:.3f}"


def sorted_status_distribution(counter: Counter[str]) -> Dict[str, int]:
    def sort_key(item: Tuple[str, int]) -> Tuple[int, str]:
        code = try_int(item[0])
        if code is None:
            return (10_000, item[0])
        return (code, item[0])

    return {k: v for k, v in sorted(counter.items(), key=sort_key)}


def endpoint_from_tags(tags: Dict[str, Any]) -> str:
    method = str(tags.get("method", "?")).upper()
    raw_url = str(tags.get("url") or tags.get("name") or "?")
    if raw_url == "?":
        return f"{method} ?"

    try:
        path = urlparse(raw_url).path or raw_url
    except Exception:
        path = raw_url
    return f"{method} {path}"


def parse_metrics_file(metrics_file: Path) -> Dict[str, Any]:
    endpoint_acc: Dict[str, EndpointAccumulator] = {}
    overall_status_counts: Counter[str] = Counter()
    total_requests = 0
    total_errors = 0

    with metrics_file.open("r", encoding="utf-8") as f:
        for raw_line in f:
            line = raw_line.strip()
            if not line:
                continue
            try:
                item = json.loads(line)
            except json.JSONDecodeError:
                continue

            if item.get("type") != "Point" or item.get("metric") != "http_req_duration":
                continue

            data = item.get("data", {})
            tags = data.get("tags", {}) or {}
            duration_ms = float(data.get("value", 0.0))
            status = str(tags.get("status", "unknown"))
            endpoint = endpoint_from_tags(tags)

            acc = endpoint_acc.setdefault(endpoint, EndpointAccumulator())
            acc.add(duration_ms, status)

            overall_status_counts[status] += 1
            total_requests += 1
            status_int = try_int(status)
            if status_int is not None and status_int >= 400:
                total_errors += 1

    per_api: List[Dict[str, Any]] = []
    for endpoint, acc in endpoint_acc.items():
        sorted_durations = sorted(acc.durations_ms)
        per_api.append(
            {
                "endpoint": endpoint,
                "request_count": acc.total_count,
                "error_count": acc.error_count,
                "error_rate": (acc.error_count / acc.total_count) if acc.total_count else 0.0,
                "p50_ms": percentile(sorted_durations, 50),
                "p95_ms": percentile(sorted_durations, 95),
                "p99_ms": percentile(sorted_durations, 99),
                "avg_ms": (sum(sorted_durations) / len(sorted_durations)) if sorted_durations else None,
                "max_ms": sorted_durations[-1] if sorted_durations else None,
                "status_distribution": sorted_status_distribution(acc.status_counts),
            }
        )

    per_api.sort(key=lambda x: (x["p95_ms"] if x["p95_ms"] is not None else -1), reverse=True)

    return {
        "total_requests": total_requests,
        "total_errors": total_errors,
        "overall_error_rate": (total_errors / total_requests) if total_requests else 0.0,
        "status_distribution": sorted_status_distribution(overall_status_counts),
        "per_api": per_api,
        "response_payload_recording": {
            "recorded": False,
            "reason": "k6 --out json captures metrics/tags (status, method, url, timings), not response bodies.",
        },
    }


def collect_failing_checks(group: Dict[str, Any], out: List[Dict[str, Any]]) -> None:
    checks = group.get("checks", {}) or {}
    for _, check_info in checks.items():
        fails = int(check_info.get("fails", 0) or 0)
        passes = int(check_info.get("passes", 0) or 0)
        if fails > 0:
            out.append(
                {
                    "name": check_info.get("name", ""),
                    "path": check_info.get("path", ""),
                    "fails": fails,
                    "passes": passes,
                }
            )

    for _, child in (group.get("groups", {}) or {}).items():
        collect_failing_checks(child, out)


def parse_summary_file(summary_file: Path) -> Dict[str, Any]:
    with summary_file.open("r", encoding="utf-8") as f:
        summary = json.load(f)

    threshold_failures: List[Dict[str, str]] = []
    for metric_name, metric_info in (summary.get("metrics", {}) or {}).items():
        thresholds = metric_info.get("thresholds")
        if not isinstance(thresholds, dict):
            continue
        for threshold_expr, ok in thresholds.items():
            if ok is False:
                threshold_failures.append(
                    {
                        "metric": metric_name,
                        "threshold": threshold_expr,
                    }
                )

    failing_checks: List[Dict[str, Any]] = []
    root_group = summary.get("root_group", {}) or {}
    collect_failing_checks(root_group, failing_checks)
    failing_checks.sort(key=lambda x: x["fails"], reverse=True)

    return {
        "threshold_failures": threshold_failures,
        "failing_checks": failing_checks,
    }


def parse_console_log(console_file: Path) -> Dict[str, Any]:
    if not console_file.exists():
        return {"common_console_issues": []}

    issue_counter: Counter[str] = Counter()
    with console_file.open("r", encoding="utf-8", errors="replace") as f:
        for raw_line in f:
            line = ANSI_RE.sub("", raw_line).strip()
            if not line:
                continue
            if "level=error" in line or "thresholds on metrics" in line or "✗" in line:
                issue_counter[line] += 1

    common = [{"message": msg, "count": count} for msg, count in issue_counter.most_common(20)]
    return {"common_console_issues": common}


def find_latest_artifact_dir(base_dir: Path) -> Optional[Path]:
    if not base_dir.exists():
        return None

    candidates = []
    for child in base_dir.iterdir():
        if not child.is_dir():
            continue
        if (child / "manifest.csv").exists():
            candidates.append(child)

    if not candidates:
        return None

    candidates.sort(key=lambda p: p.stat().st_mtime, reverse=True)
    return candidates[0]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Analyze k6 load-test artifacts and generate per-API latency/error reports."
    )
    parser.add_argument(
        "--artifact-dir",
        default="",
        help="Artifact directory containing manifest.csv (default: latest under debug-artifacts/load-tests).",
    )
    parser.add_argument(
        "--output-json",
        default="",
        help="Output JSON report path (default: <artifact-dir>/analysis/report.json).",
    )
    parser.add_argument(
        "--output-md",
        default="",
        help="Output Markdown report path (default: <artifact-dir>/analysis/report.md).",
    )
    parser.add_argument(
        "--top",
        type=int,
        default=0,
        help="Limit APIs shown in markdown per run (0 = all).",
    )
    return parser.parse_args()


def build_markdown(report: Dict[str, Any], top: int) -> str:
    lines: List[str] = []
    lines.append("# K6 Load Test Analysis")
    lines.append("")
    lines.append(f"- Generated UTC: `{report['generated_utc']}`")
    lines.append(f"- Artifact dir: `{report['artifact_dir']}`")
    lines.append(f"- Completed runs: `{report['completed_runs']}`")
    lines.append(f"- Non-zero exits: `{report['nonzero_exit_runs']}`")
    lines.append("")
    lines.append(
        "> Response bodies are **not** recorded in current k6 JSON output; only metrics and request tags are available."
    )
    lines.append("")

    for run in report["runs"]:
        lines.append(f"## {run['region'].upper()} / {run['scenario'].upper()}")
        lines.append("")
        lines.append(
            f"- Exit: `{run['exit_code']}` | Duration: `{run['duration_sec']}s` | "
            f"Requests: `{run['metrics']['total_requests']}` | "
            f"Error rate: `{run['metrics']['overall_error_rate'] * 100:.2f}%`"
        )
        lines.append(f"- Status distribution: `{run['metrics']['status_distribution']}`")
        lines.append("")

        threshold_failures = run["issues"]["threshold_failures"]
        if threshold_failures:
            lines.append("**Threshold failures**")
            for t in threshold_failures:
                lines.append(f"- `{t['metric']}`: `{t['threshold']}`")
            lines.append("")

        failing_checks = run["issues"]["failing_checks"][:10]
        if failing_checks:
            lines.append("**Top failing checks**")
            for c in failing_checks:
                lines.append(f"- `{c['name']}` fails={c['fails']} passes={c['passes']}")
            lines.append("")

        common_issues = run["issues"]["common_console_issues"][:10]
        if common_issues:
            lines.append("**Common console issues**")
            for issue in common_issues:
                lines.append(f"- ({issue['count']}x) {issue['message']}")
            lines.append("")

        lines.append("**Per-API latency and error**")
        lines.append("")
        lines.append("| API | Requests | p50 (ms) | p95 (ms) | p99 (ms) | Error rate | Status codes |")
        lines.append("|---|---:|---:|---:|---:|---:|---|")
        per_api_rows = run["metrics"]["per_api"]
        if top > 0:
            per_api_rows = per_api_rows[:top]
        for api in per_api_rows:
            lines.append(
                f"| `{api['endpoint']}` | {api['request_count']} | "
                f"{fmt_ms(api['p50_ms'])} | {fmt_ms(api['p95_ms'])} | {fmt_ms(api['p99_ms'])} | "
                f"{api['error_rate'] * 100:.2f}% | `{api['status_distribution']}` |"
            )
        lines.append("")

    return "\n".join(lines)


def main() -> int:
    args = parse_args()
    repo_root = Path.cwd()

    if args.artifact_dir:
        artifact_dir = (repo_root / args.artifact_dir).resolve()
    else:
        latest = find_latest_artifact_dir((repo_root / "debug-artifacts" / "load-tests").resolve())
        if latest is None:
            raise SystemExit("No artifact directory found with manifest.csv under debug-artifacts/load-tests")
        artifact_dir = latest

    manifest_path = artifact_dir / "manifest.csv"
    if not manifest_path.exists():
        raise SystemExit(f"manifest.csv not found at: {manifest_path}")

    output_json = Path(args.output_json).resolve() if args.output_json else artifact_dir / "analysis" / "report.json"
    output_md = Path(args.output_md).resolve() if args.output_md else artifact_dir / "analysis" / "report.md"
    output_json.parent.mkdir(parents=True, exist_ok=True)
    output_md.parent.mkdir(parents=True, exist_ok=True)

    runs: List[Dict[str, Any]] = []
    nonzero_exit_runs = 0

    aggregate_acc: Dict[str, EndpointAccumulator] = defaultdict(EndpointAccumulator)
    aggregate_status_counts: Counter[str] = Counter()
    aggregate_requests = 0
    aggregate_errors = 0

    with manifest_path.open("r", encoding="utf-8", newline="") as f:
        for row in csv.DictReader(f):
            exit_code = str(row.get("exit_code", ""))
            if exit_code != "0":
                nonzero_exit_runs += 1

            metrics_file = Path(row.get("metrics_file", ""))
            summary_file = Path(row.get("summary_file", ""))
            console_file = Path(row.get("console_log", ""))

            run_metrics: Dict[str, Any] = {
                "total_requests": 0,
                "total_errors": 0,
                "overall_error_rate": 0.0,
                "status_distribution": {},
                "per_api": [],
                "response_payload_recording": {
                    "recorded": False,
                    "reason": "k6 --out json captures metrics/tags (status, method, url, timings), not response bodies.",
                },
            }
            run_issues: Dict[str, Any] = {
                "threshold_failures": [],
                "failing_checks": [],
                "common_console_issues": [],
            }

            if metrics_file.exists():
                run_metrics = parse_metrics_file(metrics_file)
                aggregate_requests += run_metrics["total_requests"]
                aggregate_errors += run_metrics["total_errors"]
                for status, count in run_metrics["status_distribution"].items():
                    aggregate_status_counts[status] += count

                for api in run_metrics["per_api"]:
                    endpoint = api["endpoint"]
                    # Reconstruct into aggregate accumulator by replaying approximate
                    # status counts and using available percentile data is inaccurate.
                    # Instead, parse metrics file once and aggregate directly here.
                # Accurate aggregate endpoint stats are computed below from metrics file.
                with metrics_file.open("r", encoding="utf-8") as mf:
                    for raw_line in mf:
                        line = raw_line.strip()
                        if not line:
                            continue
                        try:
                            item = json.loads(line)
                        except json.JSONDecodeError:
                            continue
                        if item.get("type") != "Point" or item.get("metric") != "http_req_duration":
                            continue
                        data = item.get("data", {})
                        tags = data.get("tags", {}) or {}
                        endpoint = endpoint_from_tags(tags)
                        status = str(tags.get("status", "unknown"))
                        duration_ms = float(data.get("value", 0.0))
                        aggregate_acc[endpoint].add(duration_ms, status)

            if summary_file.exists():
                parsed_summary = parse_summary_file(summary_file)
                run_issues["threshold_failures"] = parsed_summary["threshold_failures"]
                run_issues["failing_checks"] = parsed_summary["failing_checks"]

            if console_file.exists():
                parsed_console = parse_console_log(console_file)
                run_issues["common_console_issues"] = parsed_console["common_console_issues"]

            runs.append(
                {
                    "region": row.get("region", ""),
                    "scenario": row.get("scenario", ""),
                    "base_url": row.get("base_url", ""),
                    "exit_code": try_int(exit_code) if try_int(exit_code) is not None else exit_code,
                    "duration_sec": try_int(row.get("duration_sec")) or 0,
                    "start_utc": row.get("start_utc", ""),
                    "end_utc": row.get("end_utc", ""),
                    "summary_file": str(summary_file),
                    "metrics_file": str(metrics_file),
                    "console_log": str(console_file),
                    "metrics": run_metrics,
                    "issues": run_issues,
                }
            )

    aggregate_per_api: List[Dict[str, Any]] = []
    for endpoint, acc in aggregate_acc.items():
        durations = sorted(acc.durations_ms)
        aggregate_per_api.append(
            {
                "endpoint": endpoint,
                "request_count": acc.total_count,
                "error_count": acc.error_count,
                "error_rate": (acc.error_count / acc.total_count) if acc.total_count else 0.0,
                "p50_ms": percentile(durations, 50),
                "p95_ms": percentile(durations, 95),
                "p99_ms": percentile(durations, 99),
                "avg_ms": (sum(durations) / len(durations)) if durations else None,
                "max_ms": durations[-1] if durations else None,
                "status_distribution": sorted_status_distribution(acc.status_counts),
            }
        )
    aggregate_per_api.sort(key=lambda x: (x["p95_ms"] if x["p95_ms"] is not None else -1), reverse=True)

    report = {
        "generated_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
        "artifact_dir": str(artifact_dir),
        "completed_runs": len(runs),
        "nonzero_exit_runs": nonzero_exit_runs,
        "runs": runs,
        "aggregate": {
            "total_requests": aggregate_requests,
            "total_errors": aggregate_errors,
            "overall_error_rate": (aggregate_errors / aggregate_requests) if aggregate_requests else 0.0,
            "status_distribution": sorted_status_distribution(aggregate_status_counts),
            "per_api": aggregate_per_api,
            "response_payload_recording": {
                "recorded": False,
                "reason": "k6 --out json captures metrics/tags (status, method, url, timings), not response bodies.",
            },
        },
    }

    output_json.write_text(json.dumps(report, indent=2), encoding="utf-8")
    output_md.write_text(build_markdown(report, top=max(args.top, 0)), encoding="utf-8")

    print(f"Artifact dir: {artifact_dir}")
    print(f"Completed runs: {len(runs)}")
    print(f"Non-zero exits: {nonzero_exit_runs}")
    print(f"JSON report: {output_json}")
    print(f"Markdown report: {output_md}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
