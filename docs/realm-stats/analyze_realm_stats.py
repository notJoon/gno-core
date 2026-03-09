#!/usr/bin/env python3
"""
Analyze realm_stats.log output from GNO_REALM_STATS_LOG.

Usage:
    python3 analyze_realm_stats.py <logfile>
    python3 analyze_realm_stats.py docs/realm-stats/staker_lifecycle_raw.log
"""

import re
import sys
from collections import defaultdict


def parse_log(filepath):
    with open(filepath) as f:
        lines = f.readlines()

    current_call = "Setup (AddPackage)"
    calls = []
    current_entries = []

    for line in lines:
        line = line.rstrip()
        m = re.match(r'^--- (Call|AddPackage) (.+?) ---$', line)
        if m:
            if current_entries:
                calls.append((current_call, current_entries))
            current_call = f"{m.group(1)} {m.group(2)}"
            current_entries = []
            continue

        m = re.match(
            r'^\[realm-stats\] path=(\S+)\s+'
            r'created=\s*(\d+)\s+updated=\s*(\d+)\s+'
            r'ancestors=\s*(\d+)\s+deleted=\s*(\d+)\s+'
            r'bytes=([+-]?\d+)',
            line,
        )
        if m:
            current_entries.append({
                'path': m.group(1),
                'created': int(m.group(2)),
                'updated': int(m.group(3)),
                'ancestors': int(m.group(4)),
                'deleted': int(m.group(5)),
                'bytes': int(m.group(6)),
            })

    if current_entries:
        calls.append((current_call, current_entries))

    return calls


def print_detail(calls):
    print("=" * 120)
    print("USER-FACING OPERATIONS (Call only)")
    print("=" * 120)

    seen_calls = {}
    for call_name, entries in calls:
        if not call_name.startswith("Call "):
            continue

        realm_totals = defaultdict(
            lambda: {'created': 0, 'updated': 0, 'ancestors': 0,
                     'deleted': 0, 'bytes': 0, 'entries': 0}
        )
        total = {'created': 0, 'updated': 0, 'ancestors': 0,
                 'deleted': 0, 'bytes': 0, 'entries': 0}

        for e in entries:
            for k in ['created', 'updated', 'ancestors', 'deleted', 'bytes']:
                realm_totals[e['path']][k] += e[k]
                total[k] += e[k]
            realm_totals[e['path']]['entries'] += 1
            total['entries'] += 1

        key = call_name
        if key not in seen_calls:
            seen_calls[key] = 0
        seen_calls[key] += 1
        occurrence = seen_calls[key]

        print(f"\n{'─' * 120}")
        print(f"  {call_name} (occurrence #{occurrence})")
        print(
            f"  TOTAL: created={total['created']} updated={total['updated']} "
            f"ancestors={total['ancestors']} deleted={total['deleted']} "
            f"bytes={total['bytes']:+d} | finalize_calls={total['entries']}"
        )
        print(f"{'─' * 120}")

        for realm, t in sorted(
            realm_totals.items(), key=lambda x: abs(x[1]['bytes']), reverse=True
        ):
            print(
                f"  {realm:<55s} c={t['created']:3d} u={t['updated']:3d} "
                f"a={t['ancestors']:3d} d={t['deleted']:3d} "
                f"bytes={t['bytes']:+6d} | finalize_calls={t['entries']}"
            )


def print_summary(calls):
    print("\n" + "=" * 120)
    print("SUMMARY TABLE: Per-operation totals")
    print("=" * 120)
    print(
        f"{'Operation':<55s} {'Created':>8s} {'Updated':>8s} "
        f"{'Ancestors':>10s} {'Deleted':>8s} {'Bytes':>10s} {'Finalizes':>10s}"
    )
    print("-" * 120)

    seen = {}
    for call_name, entries in calls:
        if not call_name.startswith("Call "):
            continue
        key = call_name
        if key not in seen:
            seen[key] = 0
        seen[key] += 1

        total = {'created': 0, 'updated': 0, 'ancestors': 0,
                 'deleted': 0, 'bytes': 0}
        for e in entries:
            for k in total:
                total[k] += e[k]

        label = f"{call_name} #{seen[key]}"
        print(
            f"{label:<55s} {total['created']:>8d} {total['updated']:>8d} "
            f"{total['ancestors']:>10d} {total['deleted']:>8d} "
            f"{total['bytes']:>+10d} {len(entries):>10d}"
        )


def print_zero_byte_analysis(calls):
    print("\n" + "=" * 120)
    print("ZERO-BYTE FINALIZE ANALYSIS (wasted re-serialization)")
    print("=" * 120)

    seen = {}
    for call_name, entries in calls:
        if not call_name.startswith("Call "):
            continue
        key = call_name
        if key not in seen:
            seen[key] = 0
        seen[key] += 1

        zero_byte_count = sum(
            1 for e in entries
            if e['created'] == 0 and e['deleted'] == 0
            and e['ancestors'] == 0 and e['bytes'] == 0
        )

        label = f"{call_name} #{seen[key]}"
        ratio = f"{zero_byte_count:3d} / {len(entries):3d}"
        pct = (zero_byte_count / len(entries) * 100) if entries else 0
        print(f"  {label:<55s} zero-byte finalize calls: {ratio} ({pct:5.1f}%)")


def print_addpackage_summary(calls):
    print("\n" + "=" * 120)
    print("ADDPACKAGE SUMMARY (deployment costs)")
    print("=" * 120)
    print(
        f"{'Package':<55s} {'Created':>8s} {'Updated':>8s} "
        f"{'Bytes':>10s}"
    )
    print("-" * 120)

    for call_name, entries in calls:
        if not call_name.startswith("AddPackage "):
            continue
        pkg = call_name.replace("AddPackage ", "")

        total = {'created': 0, 'updated': 0, 'bytes': 0}
        for e in entries:
            total['created'] += e['created']
            total['updated'] += e['updated']
            total['bytes'] += e['bytes']

        print(
            f"  {pkg:<53s} {total['created']:>8d} {total['updated']:>8d} "
            f"{total['bytes']:>+10d}"
        )


def main():
    if len(sys.argv) < 2:
        print(f"Usage: {sys.argv[0]} <realm_stats.log>", file=sys.stderr)
        sys.exit(1)

    filepath = sys.argv[1]
    calls = parse_log(filepath)

    print(f"Parsed {len(calls)} sections from {filepath}\n")

    print_detail(calls)
    print_summary(calls)
    print_zero_byte_analysis(calls)
    print_addpackage_summary(calls)


if __name__ == "__main__":
    main()
