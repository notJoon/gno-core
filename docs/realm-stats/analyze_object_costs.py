#!/usr/bin/env python3
"""
Analyze per-object storage costs from GNO_REALM_STATS_LOG output.

Requires the [obj-cost] logging instrumentation in realm.go.
Parses each transaction (Call) section and breaks down costs by realm,
operation type, and object type.

Usage:
    # Analyze a specific function call in the log:
    python3 analyze_object_costs.py <logfile> <call_pattern>

    # Examples:
    python3 analyze_object_costs.py /tmp/realm_stats.log "position.Mint"
    python3 analyze_object_costs.py /tmp/realm_stats.log "pool.CreatePool"
    python3 analyze_object_costs.py /tmp/realm_stats.log "position.DecreaseLiquidity"

    # List all Call sections in the log:
    python3 analyze_object_costs.py <logfile> --list

    # Analyze the Nth occurrence of a pattern (default: 1st):
    python3 analyze_object_costs.py <logfile> "position.Mint" --nth 2

Generating the log:
    GNO_REALM_STATS_LOG=/tmp/realm_stats.log go test -v -run 'TestTestdata/your_test' ./gno.land/pkg/integration/ -count=1
    GNO_REALM_STATS_LOG=stderr go test -run 'TestFiles/zrealm_avl0' ./gnovm/pkg/gnolang/ -v -count=1
"""

import re
import sys
from collections import defaultdict

# Compiled regexes for performance
OBJ_RE = re.compile(
    r'\[obj-cost\]\s+op=(\S+)\s+type=(\S+)\s+oid=(\S+)\s+owner=(\S+)'
    r'\s+size=\s*(\d+)\s+diff=\s*([+-]?\d+)\s+running=\s*([+-]?\d+)\s*(.*)'
)
RS_RE = re.compile(
    r'\[realm-stats\]\s+path=(\S+)\s+created=\s*(\d+)\s+updated=\s*(\d+)'
    r'\s+ancestors=\s*(\d+)\s+deleted=\s*(\d+)\s+bytes=\s*([+-]?\d+)'
)
TRIG_RE = re.compile(
    r'\[finalize-trigger\]\s+realm=(\S+)\s+func=(\S+)'
)
CALL_RE = re.compile(r'^--- (Call|AddPackage) (.+?) ---$')


def find_call_boundaries(lines, pattern, nth=1):
    """Find start/end line indices for the Nth occurrence of a Call matching pattern."""
    found = 0
    start = None
    for i, line in enumerate(lines):
        m = CALL_RE.match(line.strip())
        if m and pattern in m.group(2):
            found += 1
            if found == nth:
                start = i
            elif start is not None:
                return start, i
        elif start is not None and CALL_RE.match(line.strip()):
            return start, i
    if start is not None:
        return start, len(lines)
    return None, None


def list_calls(lines):
    """List all Call/AddPackage sections in the log."""
    calls = []
    for i, line in enumerate(lines):
        m = CALL_RE.match(line.strip())
        if m:
            calls.append((i + 1, m.group(1), m.group(2)))
    return calls


def parse_sections(lines):
    """
    Parse [obj-cost], [realm-stats], and [finalize-trigger] lines
    into per-realm finalization sections.

    Log ordering per finalization:
      [finalize-trigger] ... (0+, triggers for the upcoming realm)
      [obj-cost] ...         (0+, per-object cost entries)
      [realm-stats] ...      (exactly 1, marks end of this realm's section)
      [created]/[updated]/[deleted] type summaries (ignored)
    """
    sections = []
    last_triggers = {}
    pending_objs = []

    for line in lines:
        m = TRIG_RE.search(line)
        if m:
            last_triggers[m.group(1)] = m.group(2)
            continue

        m = OBJ_RE.search(line)
        if m:
            pending_objs.append({
                "op": m.group(1),
                "type": m.group(2),
                "oid": m.group(3),
                "owner": m.group(4),
                "size": int(m.group(5)),
                "diff": int(m.group(6)),
                "running": int(m.group(7)),
                "label": m.group(8).strip(),
            })
            continue

        m = RS_RE.search(line)
        if m:
            realm = m.group(1)
            sections.append({
                "realm": realm,
                "func": last_triggers.get(realm, ""),
                "created": int(m.group(2)),
                "updated": int(m.group(3)),
                "ancestors": int(m.group(4)),
                "deleted": int(m.group(5)),
                "bytes": int(m.group(6)),
                "objs": list(pending_objs),
            })
            pending_objs = []

    return sections


def short_realm(realm):
    return (realm
            .replace("gno.land/r/", "r/")
            .replace("gno.land/p/", "p/"))


def short_func(func_path):
    return func_path.split(".")[-1] if func_path else "(exit)"


def print_summary(sections, title=""):
    total = sum(s["bytes"] for s in sections)
    abs_all = sum(sum(abs(o["diff"]) for o in s["objs"]) for s in sections)

    if title:
        print(f"\n{title}")
    print(f"COST SUMMARY (total net: {total:+d} bytes, abs churn: {abs_all:d} bytes)")
    print("=" * 115)
    print(f"{'Realm':<42} {'Trigger':<22} {'Cr':>3} {'Up':>3} {'An':>3} {'Del':>3}"
          f" {'Net':>8} {'|Churn|':>8}")
    print("-" * 115)

    for s in sorted(sections, key=lambda x: abs(x["bytes"]), reverse=True):
        sr = short_realm(s["realm"])
        sf = short_func(s["func"])
        abs_cost = sum(abs(o["diff"]) for o in s["objs"])
        print(f"{sr:<42} {sf:<22} {s['created']:3d} {s['updated']:3d}"
              f" {s['ancestors']:3d} {s['deleted']:3d}"
              f" {s['bytes']:+8d} {abs_cost:8d}")

    print("-" * 115)
    print(f"{'TOTAL':<42} {'':<22} {'':>3} {'':>3} {'':>3} {'':>3}"
          f" {total:+8d} {abs_all:8d}")


def print_detail(sections, top_n_objects=5):
    for s in sorted(sections, key=lambda x: abs(x["bytes"]), reverse=True):
        if not s["objs"]:
            continue

        sr = short_realm(s["realm"])
        sf = short_func(s["func"])
        print(f"\n{'=' * 115}")
        print(f"REALM: {sr} | trigger: {sf} | net={s['bytes']:+d} bytes")
        print(f"  created={s['created']} updated={s['updated']}"
              f" ancestors={s['ancestors']} deleted={s['deleted']}")
        print("=" * 115)

        for op_name in ["create", "update", "ancestor", "delete"]:
            items = [o for o in s["objs"] if o["op"] == op_name]
            if not items:
                continue
            op_bytes = sum(o["diff"] for o in items)
            print(f"\n  [{op_name.upper()}] {len(items)} objects, {op_bytes:+d} bytes")

            by_type = defaultdict(list)
            for item in items:
                by_type[item["type"]].append(item)

            for typ, objs in sorted(
                by_type.items(),
                key=lambda x: sum(abs(o["diff"]) for o in x[1]),
                reverse=True,
            ):
                td = sum(o["diff"] for o in objs)
                ts = sum(o["size"] for o in objs)
                avg = ts // len(objs) if objs else 0
                print(f"    {typ:<22} x{len(objs):<3}"
                      f" diff={td:>+7} avg_size={avg:>5}")
                for o in sorted(
                    objs, key=lambda x: abs(x["diff"]), reverse=True
                )[:top_n_objects]:
                    lbl = f"  {o['label']}" if o["label"] else ""
                    oid_short = o["oid"].split(":")[-1]
                    print(f"      oid=...:{oid_short:<6}"
                          f" size={o['size']:>6} diff={o['diff']:>+6}{lbl}")
                if len(objs) > top_n_objects:
                    print(f"      ... +{len(objs) - top_n_objects} more")


def extract_root_var(label):
    """Extract root=<varname> from the label string."""
    m = re.search(r'root=(\S+)', label)
    return m.group(1) if m else "(unknown)"


def print_by_var(sections):
    """Group all objects across realms by (realm, root_var) and show cost."""
    var_costs = defaultdict(lambda: {"create": 0, "update": 0, "ancestor": 0,
                                     "delete": 0, "count": 0, "abs": 0})
    for s in sections:
        sr = short_realm(s["realm"])
        for o in s["objs"]:
            root = extract_root_var(o["label"])
            key = f"{sr}.{root}"
            var_costs[key][o["op"]] += o["diff"]
            var_costs[key]["count"] += 1
            var_costs[key]["abs"] += abs(o["diff"])

    print(f"\n{'=' * 115}")
    print("COST BY VARIABLE (realm.variable)")
    print("=" * 115)
    print(f"{'Variable':<55} {'Create':>8} {'Update':>8} {'Ancestor':>8}"
          f" {'Delete':>8} {'Net':>8} {'|Churn|':>8} {'#Obj':>5}")
    print("-" * 115)

    total_net = 0
    total_abs = 0
    for key in sorted(var_costs.keys(),
                      key=lambda k: abs(sum(v for vk, v in var_costs[k].items()
                                            if vk not in ("count", "abs"))),
                      reverse=True):
        v = var_costs[key]
        net = v["create"] + v["update"] + v["ancestor"] + v["delete"]
        total_net += net
        total_abs += v["abs"]
        print(f"{key:<55} {v['create']:+8d} {v['update']:+8d}"
              f" {v['ancestor']:+8d} {v['delete']:+8d}"
              f" {net:+8d} {v['abs']:8d} {v['count']:5d}")

    print("-" * 115)
    print(f"{'TOTAL':<55} {'':>8} {'':>8} {'':>8} {'':>8}"
          f" {total_net:+8d} {total_abs:8d}")


def main():
    if len(sys.argv) < 2:
        print(__doc__)
        sys.exit(1)

    logfile = sys.argv[1]
    with open(logfile) as f:
        lines = f.readlines()

    # --list mode
    if len(sys.argv) >= 3 and sys.argv[2] == "--list":
        calls = list_calls(lines)
        print(f"Found {len(calls)} Call/AddPackage sections:\n")
        for lineno, kind, name in calls:
            print(f"  L{lineno:<6d} [{kind:<10s}] {name}")
        return

    if len(sys.argv) < 3:
        print("Usage: analyze_object_costs.py <logfile> <call_pattern> [--nth N] [--by-var]")
        print("       analyze_object_costs.py <logfile> --list")
        sys.exit(1)

    pattern = sys.argv[2]
    nth = 1
    if "--nth" in sys.argv:
        idx = sys.argv.index("--nth")
        nth = int(sys.argv[idx + 1])

    by_var = "--by-var" in sys.argv

    start, end = find_call_boundaries(lines, pattern, nth)
    if start is None:
        print(f"ERROR: No Call section matching '{pattern}' found (nth={nth})")
        sys.exit(1)

    ordinal = {1: "1st", 2: "2nd", 3: "3rd"}.get(nth, f"{nth}th")
    title = f"=== {ordinal} Call matching '{pattern}': lines {start + 1}-{end} ==="
    print(title)

    sections = parse_sections(lines[start:end])
    if not sections:
        print("No realm-stats sections found in this range.")
        return

    print_summary(sections)
    if by_var:
        print_by_var(sections)
    else:
        print_detail(sections)


if __name__ == "__main__":
    main()
