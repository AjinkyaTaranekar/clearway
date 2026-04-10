#!/usr/bin/env python3
"""
analyze-chaos-results.py — Aggregate chaos suite run data into a report.

Reads each experiment's timeline.csv and report.md produced by chaos-monkey.sh,
computes per-experiment metrics (outage duration, recovery time, unexpected failures),
and writes a combined markdown report to the suite directory.

Usage:
    python3 scripts/chaos/analyze-chaos-results.py \
        --suite-dir debug-artifacts/chaos/suite-20260410T... \
        --manifest  debug-artifacts/chaos/suite-20260410T.../manifest.csv
"""

import argparse
import csv
import os
import re
import sys
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Optional


# ── Data classes ──────────────────────────────────────────────────────────────

@dataclass
class PollSnapshot:
    timestamp: str
    elapsed_s: int
    eu_health: str
    us_health: str
    apac_health: str
    endpoint_status: str
    notes: str = ""


@dataclass
class ExperimentMetrics:
    num: int
    phase: str
    label: str
    experiment_type: str
    report_dir: str
    suite_result: str           # PASS / FAIL(N)

    # Derived from timeline.csv
    total_polls: int = 0
    endpoint_up_polls: int = 0
    endpoint_down_polls: int = 0
    outage_duration_s: int = 0       # seconds endpoint was not UP
    first_down_at_s: Optional[int] = None
    first_up_after_restore_s: Optional[int] = None
    non_target_cell_fails: int = 0   # polls where a non-target cell went down
    all_cells_down_polls: int = 0

    # From report.md
    abort_triggered: bool = False
    abort_reason: str = ""
    key_events: list = field(default_factory=list)

    # Computed
    @property
    def availability_pct(self) -> float:
        if self.total_polls == 0:
            return 0.0
        return 100.0 * self.endpoint_up_polls / self.total_polls

    @property
    def recovery_time_s(self) -> Optional[int]:
        """Seconds from end of chaos window until endpoint first came back UP."""
        return self.first_up_after_restore_s

    @property
    def status_icon(self) -> str:
        if self.abort_triggered:
            return "⚠ ABORT"
        if self.suite_result.startswith("PASS"):
            return "✓ PASS"
        return "✗ FAIL"

    @property
    def collateral_damage(self) -> bool:
        return self.non_target_cell_fails > 0


# ── Parsers ───────────────────────────────────────────────────────────────────

def parse_manifest(path: Path) -> list[dict]:
    rows = []
    with path.open() as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append(row)
    return rows


def parse_timeline(csv_path: Path) -> list[PollSnapshot]:
    snapshots = []
    if not csv_path.exists():
        return snapshots
    with csv_path.open() as f:
        reader = csv.DictReader(f)
        for row in reader:
            try:
                snapshots.append(PollSnapshot(
                    timestamp=row.get("timestamp", ""),
                    elapsed_s=int(row.get("elapsed_s", 0)),
                    eu_health=row.get("eu_health", ""),
                    us_health=row.get("us_health", ""),
                    apac_health=row.get("apac_health", ""),
                    endpoint_status=row.get("endpoint_status", ""),
                    notes=row.get("notes", ""),
                ))
            except (ValueError, KeyError):
                continue
    return snapshots


def parse_report_md(report_path: Path) -> dict:
    """Extract key fields from chaos-monkey.sh's report.md."""
    data = {"abort": False, "abort_reason": "", "events": [], "down_pct": 0}
    if not report_path.exists():
        return data
    text = report_path.read_text()

    if "EARLY ABORT TRIGGERED" in text:
        data["abort"] = True
        m = re.search(r"Reason: (.+)", text)
        if m:
            data["abort_reason"] = m.group(1).strip()

    # Extract key events
    in_events = False
    for line in text.splitlines():
        if line.startswith("## Key Events"):
            in_events = True
            continue
        if in_events:
            if line.startswith("##"):
                break
            stripped = line.strip()
            if stripped and stripped != "none":
                data["events"].append(stripped)

    # Extract down percentage
    m = re.search(r"Down %.*?(\d+)%", text)
    if m:
        data["down_pct"] = int(m.group(1))

    return data


def compute_metrics(row: dict) -> ExperimentMetrics:
    m = ExperimentMetrics(
        num=int(row["num"]),
        phase=row["phase"],
        label=row["label"],
        experiment_type=row["experiment_type"],
        report_dir=row["report_dir"],
        suite_result=row["result"],
    )

    run_dir = Path(row["report_dir"])
    if not run_dir.exists():
        return m

    # Timeline analysis
    timeline = parse_timeline(run_dir / "timeline.csv")
    m.total_polls = len(timeline)

    # The chaos window ends when restore is triggered (last poll before recovery).
    # We track two phases: chaos window (endpoint should be DOWN) and recovery.
    # Simple heuristic: endpoint is expected down from first_down until first UP
    # after at least 2 down polls (avoids treating transient blips as full outages).
    down_streak = 0
    for snap in timeline:
        is_up = snap.endpoint_status.startswith("UP")
        is_down = not is_up and snap.endpoint_status not in ("", "UNREACHABLE")
        is_unreachable = snap.endpoint_status == "UNREACHABLE"

        if is_up:
            m.endpoint_up_polls += 1
            down_streak = 0
            if m.first_down_at_s is not None and m.first_up_after_restore_s is None:
                m.first_up_after_restore_s = snap.elapsed_s
        else:
            m.endpoint_down_polls += 1
            down_streak += 1
            if down_streak >= 2 and m.first_down_at_s is None:
                m.first_down_at_s = snap.elapsed_s

        if is_unreachable or is_down:
            m.outage_duration_s += 10  # approximate (poll interval)

        # Non-target cell failures
        # For scale-service, any cell going unreachable is unexpected
        unexpected = 0
        for cell_status in [snap.eu_health, snap.us_health, snap.apac_health]:
            if cell_status == "unreachable":
                unexpected += 1
        if unexpected >= 3:
            m.all_cells_down_polls += 1
        elif unexpected >= 1:
            m.non_target_cell_fails += 1

    # Parse report.md
    report_data = parse_report_md(run_dir / "report.md")
    m.abort_triggered = report_data["abort"]
    m.abort_reason = report_data["abort_reason"]
    m.key_events = report_data["events"]

    return m


# ── Known impacts reference ───────────────────────────────────────────────────

EXPECTED_IMPACT = {
    "capacity-service": (
        "Journey creates fail (502 from journey-service). "
        "Circuit breaker opens after 10 consecutive failures (~10–15s under load). "
        "**Observable:** `/api/v1/capacity/segments` → ERROR (502)."
    ),
    "map-service": (
        "Journey creates fail (map route unavailable). "
        "CB opens after 10 consecutive failures. "
        "**Observable:** `/api/v1/map/segments` → ERROR (502)."
    ),
    "iam-service": (
        "Login and registration fail on this node. "
        "JWKS endpoint served from memory — token verification still works. "
        "**Observable:** `/.well-known/jwks.json` → ERROR (502)."
    ),
    "notification-service": (
        "Push notifications not sent. Journey creates and capacity ops unaffected. "
        "**Observable:** `/api/v1/notifications` → ERROR (502)."
    ),
    "redis": (
        "HTTP endpoints stay UP (cache falls back to DB — graceful). "
        "**Silent failure:** `journey.events` Stream consumer stops; "
        "completed journeys do NOT release capacity → capacity leak. "
        "Route cache bypassed → more OSRM calls. "
        "**Observable:** endpoint stays UP (200) — check stream backlog post-restore."
    ),
    "db": (
        "All services on this node lose DB access. "
        "Reads may be cached briefly; writes fail immediately. "
        "Other cells unaffected. CRDB quorum maintained. "
        "**Observable:** `/api/v1/capacity/segments` → ERROR (500)."
    ),
    "drain-node": (
        "All services on the worker node stop. "
        "EU manager still serves. US and APAC unaffected. "
        "**Observable:** EU cell may briefly show degraded; other cells healthy."
    ),
    "block-firewall": (
        "Internet traffic blocked to the worker node. "
        "Internal Docker overlay traffic unaffected. "
        "GCP LB stops routing to this backend. "
        "**Observable:** EU cell may briefly show degraded."
    ),
}

RECOVERY_NOTES = {
    "capacity-service": "CB closes after 30s probe window with 3 successes. Fast.",
    "map-service": "CB closes after 30s probe window with 3 successes. Fast.",
    "iam-service": "Service restarts and reconnects; sessions already issued remain valid.",
    "notification-service": "Service restarts; missed notifications not retried.",
    "redis": (
        "Consumer group reconnects; outbox relay re-publishes backed-up events. "
        "Stream backlog clears within 1–2 poll cycles. "
        "Route cache warms up on first cache-miss calls."
    ),
    "db": (
        "Services auto-reconnect to local CRDB node once it restarts. "
        "No data loss (CRDB Raft log intact). "
        "Connection pool may need full TTL to cycle."
    ),
    "drain-node": "Node set back to 'active'; Swarm reschedules tasks within ~30s.",
    "block-firewall": "Firewall rule deleted; traffic restores within seconds.",
}


# ── Report generation ─────────────────────────────────────────────────────────

def render_report(metrics: list[ExperimentMetrics], suite_dir: Path) -> str:
    now = datetime.now().strftime("%Y-%m-%dT%H:%M:%S")
    total = len(metrics)
    passed = sum(1 for m in metrics if m.suite_result.startswith("PASS"))
    failed = total - passed
    aborted = sum(1 for m in metrics if m.abort_triggered)
    collateral = sum(1 for m in metrics if m.collateral_damage)

    overall = "PASS" if failed == 0 and aborted == 0 else ("ABORTED" if aborted > 0 else "FAIL")

    lines = [
        "# VCS Chaos Suite — Combined Report",
        "",
        f"| | |",
        f"|---|---|",
        f"| **Generated** | {now} |",
        f"| **Suite directory** | `{suite_dir}` |",
        f"| **Total experiments** | {total} |",
        f"| **Passed** | {passed} |",
        f"| **Failed** | {failed} |",
        f"| **Aborted (early exit)** | {aborted} |",
        f"| **Collateral damage** | {collateral} experiments affected non-target cells |",
        f"| **Overall result** | **{overall}** |",
        "",
        "---",
        "",
        "## Experiment Results Summary",
        "",
        "| # | Phase | Service | Type | Availability | Outage | Recovery | Non-target fails | Result |",
        "|---|-------|---------|------|-------------|--------|----------|-----------------|--------|",
    ]

    for m in metrics:
        avail = f"{m.availability_pct:.0f}%" if m.total_polls > 0 else "N/A"
        outage = f"{m.outage_duration_s}s" if m.outage_duration_s > 0 else "—"
        recovery = f"{m.recovery_time_s}s" if m.recovery_time_s else "—"
        non_tgt = str(m.non_target_cell_fails) if m.non_target_cell_fails > 0 else "✓ 0"
        icon = m.status_icon
        lines.append(
            f"| {m.num} | {m.phase} | {m.label} | {m.experiment_type} "
            f"| {avail} | {outage} | {recovery} | {non_tgt} | {icon} |"
        )

    lines += [
        "",
        "---",
        "",
        "## Per-Experiment Analysis",
        "",
    ]

    for m in metrics:
        label_key = m.label
        lines += [
            f"### [{m.num}] {m.label}  _(Phase {m.phase} — {m.experiment_type})_",
            "",
        ]

        if m.total_polls == 0:
            lines.append("_No timeline data available (dry-run or report directory missing)._")
            lines.append("")
            continue

        # Status
        if m.abort_triggered:
            lines += [
                f"> **EARLY ABORT** triggered during this experiment.",
                f"> Reason: {m.abort_reason}",
                "",
            ]

        # Metrics table
        lines += [
            f"| Metric | Value |",
            f"|--------|-------|",
            f"| Total polls | {m.total_polls} |",
            f"| Endpoint UP polls | {m.endpoint_up_polls} ({m.availability_pct:.0f}%) |",
            f"| Endpoint DOWN/ERROR polls | {m.endpoint_down_polls} |",
            f"| Outage duration (approx) | {m.outage_duration_s}s |",
            f"| First DOWN at | {m.first_down_at_s}s elapsed |" if m.first_down_at_s is not None else "| First DOWN at | not observed |",
            f"| Endpoint UP after restore | {m.first_up_after_restore_s}s elapsed |" if m.first_up_after_restore_s is not None else "| Endpoint UP after restore | not observed |",
            f"| Non-target cell failures | {m.non_target_cell_fails} polls |",
            f"| All-cells-down polls | {m.all_cells_down_polls} |",
            "",
        ]

        # Expected impact
        impact = EXPECTED_IMPACT.get(label_key, "")
        if impact:
            lines += [
                "**Expected impact:**",
                "",
                impact,
                "",
            ]

        # Actual vs expected
        if m.label in ("redis",):
            lines += [
                "**Redis note:** Endpoint showing UP (200) during outage is _expected behaviour_ "
                "(cache degrades to DB). The critical signal is the stream backlog check "
                "after restore — see key events below.",
                "",
            ]

        # Recovery notes
        rec = RECOVERY_NOTES.get(label_key, "")
        if rec:
            lines += [
                "**Recovery path:**",
                "",
                rec,
                "",
            ]

        # Key events from report.md
        if m.key_events:
            lines.append("**Key events (from chaos-monkey.sh report):**")
            lines.append("")
            for evt in m.key_events:
                lines.append(f"- {evt}")
            lines.append("")

        # Collateral damage warning
        if m.collateral_damage:
            lines += [
                f"> ⚠ **Unexpected:** {m.non_target_cell_fails} poll(s) recorded a "
                f"non-target cell as unreachable. Review the timeline CSV for details.",
                "",
            ]

        # Link to raw files
        if m.report_dir and Path(m.report_dir).exists():
            lines += [
                f"_Raw files: [`report.md`]({m.report_dir}/report.md) · "
                f"[`timeline.csv`]({m.report_dir}/timeline.csv)_",
                "",
            ]

        lines.append("---")
        lines.append("")

    # ── Circuit breaker analysis ──────────────────────────────────────────────
    lines += [
        "## Circuit Breaker Analysis",
        "",
        "Journey-service circuit breaker configuration (as of last update):",
        "",
        "| Parameter | Value |",
        "|-----------|-------|",
        "| Consecutive failures to open | 10 |",
        "| Open duration (timeout) | 30s |",
        "| Half-open probe requests | 3 |",
        "| HTTP client timeout | 15s |",
        "",
        "Under K6 concurrent load, the CB opens within ~10–15s of a downstream service "
        "going to 0 tasks. Without concurrent traffic the CB state is not observable "
        "via curl — the endpoint probe only confirms gateway reachability.",
        "",
    ]

    # ── Redis Streams specific section ────────────────────────────────────────
    redis_runs = [m for m in metrics if m.label == "redis"]
    if redis_runs:
        lines += [
            "## Redis Streams — Capacity Leak Risk",
            "",
            "The `journey.events` Redis Stream is the mechanism by which journey "
            "completions and cancellations release reserved capacity slots. "
            "When Redis goes to 0 tasks:",
            "",
            "1. `capacity-service` consumer group (`XReadGroup`) stops receiving events.",
            "2. `journey-service` outbox relay (`XADD`) fails; events buffer in the DB outbox.",
            "3. Capacity slots for completed journeys remain reserved until Redis recovers.",
            "4. **Risk:** if Redis is down during a high-volume period, the backlog of "
               "un-released slots can make segments appear at capacity when they are not.",
            "",
            "**Recovery:** Once Redis restarts, the outbox relay re-publishes buffered events "
            "and the consumer group processes them within 1–2 poll cycles (~10–20s). "
            "Verify with: `redis-cli XPENDING journey.events capacity-consumer-group - + 10`",
            "",
        ]

    # ── Recommendations ───────────────────────────────────────────────────────
    lines += [
        "## Recommendations",
        "",
    ]

    recs = []

    if any(m.collateral_damage for m in metrics):
        recs.append(
            "**Collateral damage detected** in one or more experiments. "
            "Review non-target cell failures — may indicate network or Swarm instability "
            "unrelated to the injected fault."
        )

    if any(m.abort_triggered for m in metrics):
        recs.append(
            "**Early abort triggered** — the experiment detected unexpected infrastructure "
            "degradation and stopped to prevent further damage. Review the abort reason "
            "in the per-experiment section above."
        )

    cap_runs = [m for m in metrics if m.label == "capacity-service" and m.total_polls > 0]
    if cap_runs:
        m = cap_runs[0]
        if m.first_down_at_s is not None and m.first_down_at_s > 30:
            recs.append(
                f"**Slow capacity-service outage detection:** endpoint took {m.first_down_at_s}s "
                "to show DOWN. nginx DNS TTL is 10s — expected detection within 10–20s. "
                "If consistently slower, check nginx `resolver valid=` setting."
            )

    redis_runs = [m for m in metrics if m.label == "redis"]
    for rm in redis_runs:
        if "stream_backlog_detected" in " ".join(rm.key_events):
            recs.append(
                "**Redis Stream backlog detected after restore.** "
                "Consider adding a Redis health probe to the monitoring stack so "
                "Stream consumer downtime is alerted before capacity leaks accumulate."
            )

    if not recs:
        recs.append(
            "No critical issues found. System demonstrated expected fault-tolerance "
            "behaviour across all tested failure scenarios."
        )

    for rec in recs:
        lines.append(f"- {rec}")

    lines += [
        "",
        "---",
        "",
        "_Generated by `scripts/chaos/analyze-chaos-results.py`_",
    ]

    return "\n".join(lines)


# ── Entry point ───────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="Analyze chaos suite results")
    parser.add_argument("--suite-dir", required=True, help="Suite output directory")
    parser.add_argument("--manifest",  required=True, help="Suite manifest CSV")
    args = parser.parse_args()

    suite_dir = Path(args.suite_dir)
    manifest_path = Path(args.manifest)

    if not manifest_path.exists():
        print(f"ERROR: manifest not found: {manifest_path}", file=sys.stderr)
        sys.exit(1)

    print(f"[analyze] Reading manifest: {manifest_path}")
    manifest_rows = parse_manifest(manifest_path)

    if not manifest_rows:
        print("[analyze] No experiments found in manifest.", file=sys.stderr)
        sys.exit(1)

    print(f"[analyze] Processing {len(manifest_rows)} experiment(s)...")
    all_metrics = []
    for row in manifest_rows:
        print(f"  [{row['num']}] {row['label']} — {row['report_dir']}")
        m = compute_metrics(row)
        all_metrics.append(m)

    report_text = render_report(all_metrics, suite_dir)

    report_path = suite_dir / "combined-report.md"
    report_path.write_text(report_text)
    print(f"\n[analyze] Combined report written: {report_path}")

    # Quick terminal summary
    print("\n" + "═" * 64)
    print("CHAOS SUITE ANALYSIS SUMMARY")
    print("═" * 64)
    print(f"{'#':<4} {'Label':<24} {'Avail':>6}  {'Outage':>8}  {'Recovery':>10}  Result")
    print(f"{'─'*4} {'─'*24} {'─'*6}  {'─'*8}  {'─'*10}  {'─'*12}")
    for m in all_metrics:
        avail = f"{m.availability_pct:.0f}%" if m.total_polls > 0 else "N/A"
        outage = f"{m.outage_duration_s}s" if m.outage_duration_s else "—"
        recovery = f"{m.recovery_time_s}s" if m.recovery_time_s else "—"
        print(f"{m.num:<4} {m.label:<24} {avail:>6}  {outage:>8}  {recovery:>10}  {m.status_icon}")
    print("═" * 64)


if __name__ == "__main__":
    main()
