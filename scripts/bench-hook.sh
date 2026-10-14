#!/usr/bin/env bash
set -euo pipefail
bin="${1:-./bin/crew}"
[ -x "$bin" ] || make build >/dev/null
bin="$(cd "$(dirname "$bin")" && pwd)/$(basename "$bin")"
home="$(mktemp -d /tmp/aws-bench.XXXXXX)"
export CREW_HOME="$home" TMUX_PANE="%0"
trap '"$bin" daemon stop >/dev/null 2>&1 || true; rm -rf "$home"' EXIT

measure() {
  python3 - "$bin" "$1" "$2" <<'PY'
import subprocess, sys, time
import os
bin, label = sys.argv[1], sys.argv[2]
budget = float(sys.argv[3]) * float(os.environ.get("BUDGET_SCALE", "1"))
payload = b'{"session_id":"bench","tool_name":"Bash","tool_input":{"command":"ls"}}'
cmd = [bin, "hook", "--harness", "claude", "--event", "PreToolUse"]
def once():
    t = time.perf_counter()
    subprocess.run(cmd, input=payload, check=True)
    return (time.perf_counter() - t) * 1000
for _ in range(20):
    once()
rounds = []
for _ in range(3):
    runs = sorted(once() for _ in range(200))
    rounds.append((runs[189], runs[99]))
p95, p50 = min(rounds)
ok = p95 < budget
spread = ", ".join(f"{r[0]:.1f}" for r in rounds)
print(f"daemon {label}: p50 {p50:.1f} ms, p95 {p95:.1f} ms (round p95s {spread}; budget p95 < {budget:.0f} ms) {'ok' if ok else 'OVER BUDGET'}")
sys.exit(0 if ok else 1)
PY
}

"$bin" daemon start >/dev/null
status=0
measure up 20 || status=1
"$bin" daemon stop >/dev/null
measure down 60 || status=1
exit $status
