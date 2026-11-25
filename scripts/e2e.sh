#!/usr/bin/env bash
# =====================================================================
# End-to-end test. It drives every strategy through the Makefile and
# checks, using the dashboard's /api/state JSON, that the traffic
# behaves the way the lectures say it does.
# Needs: a running cluster with the dashboard (`make up`), curl, jq.
# =====================================================================
set -euo pipefail

STATE_URL="${STATE_URL:-http://localhost:30080/api/state}"
TIMEOUT="${TIMEOUT:-240}"
SAMPLES="$(mktemp)"
trap 'rm -f "$SAMPLES"' EXIT

# jq helpers available in every expression:
#   pct("v2")    → % of the recent requests answered by v2
#   errs("v3")   → failed requests answered by v3
#   ready("v2")  → number of Ready pods running v2
#   pods("v2")   → number of pods (any state) running v2
JQ_DEFS='
def pct($v):   ([.traffic.byVersion[] | select(.version == $v) | .percent] | add // 0);
def errs($v):  ([.traffic.byVersion[] | select(.version == $v) | .errors]  | add // 0);
def ready($v): ([.pods[] | select(.version == $v and .status == "Ready")] | length);
def pods($v):  ([.pods[] | select(.version == $v)] | length);
'

log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
pass() { printf '    \033[32m✔ %s\033[0m\n' "$*"; }
fail() { printf '    \033[31m✘ %s\033[0m\n' "$*"; dump; exit 1; }
dump() { curl -s "$STATE_URL" | jq '{traffic: .traffic, pods: [.pods[] | {name, version, status, hits}]}' || true; }
state() { curl -sf "$STATE_URL"; }

# wait_for "<description>" '<jq boolean>' [timeout]: poll until the expression is true.
wait_for() {
  local desc="$1" expr="$2" deadline=$((SECONDS + ${3:-$TIMEOUT}))
  until state | jq -e "$JQ_DEFS $expr" >/dev/null 2>&1; do
    (( SECONDS > deadline )) && fail "timeout: $desc"
    sleep 1
  done
  pass "$desc"
}

# observe '<jq boolean>': record one snapshot per second into $SAMPLES until the expression is true.
observe() {
  local expr="$1" deadline=$((SECONDS + TIMEOUT)) snap
  : > "$SAMPLES"
  while true; do
    snap="$(state)" || { sleep 1; continue; }
    echo "$snap" | jq -c . >> "$SAMPLES"
    echo "$snap" | jq -e "$JQ_DEFS $expr" >/dev/null 2>&1 && break
    (( SECONDS > deadline )) && fail "timeout while observing: $expr"
    sleep 1
  done
}
# seen / never: assertions over the recorded samples.
seen()  { if jq -s -e "$JQ_DEFS any(.[]; $2)" "$SAMPLES" >/dev/null; then pass "$1"; else fail "never observed: $1"; fi; }
never() { if jq -s -e "$JQ_DEFS all(.[]; ($2) | not)" "$SAMPLES" >/dev/null; then pass "$1"; else fail "unexpectedly observed: $1"; fi; }

state >/dev/null || { echo "dashboard not reachable at $STATE_URL, run 'make up' first"; exit 1; }

# ------------------------------------------------------------------ recreate
log "Recreate: v1 → v2 should cause downtime"
make -s recreate-start >/dev/null
wait_for "100% of traffic on v1" 'pct("v1") >= 99'
make -s upgrade TO=v2 >/dev/null & UPGRADE=$!
observe 'pct("v2") >= 99'
wait "$UPGRADE"
seen  "requests failed while no pod was ready" '.traffic.errors > 0'
never "v1 and v2 pods were never ready at the same time" 'ready("v1") > 0 and ready("v2") > 0'

# ------------------------------------------------------------------ rolling
log "Rolling update: v1 → v2 without errors, both versions serve during the rollout"
make -s rolling-start >/dev/null
wait_for "100% of traffic on v1" 'pct("v1") >= 99'
make -s upgrade TO=v2 >/dev/null & UPGRADE=$!
observe 'pct("v2") >= 99 and pods("v1") == 0'
wait "$UPGRADE"
seen  "v1 and v2 served at the same time" 'pct("v1") > 0 and pct("v2") > 0'
never "no failed requests"                '.traffic.errors > 0'

log "Rolling update to a BAD version (v3): readiness passes, errors reach users"
make -s upgrade TO=v3 >/dev/null
wait_for "v3 serves all traffic and returns errors" 'pct("v3") >= 99 and errs("v3") > 0'
make -s undo >/dev/null
wait_for "undo brings v2 back" 'pct("v2") >= 99'

log "Rolling update to a NEVER-READY version (v4): rollout gets stuck, users unaffected"
make -s upgrade TO=v4 >/dev/null 2>&1 & UPGRADE=$!
wait_for "a v4 pod exists but never becomes ready" 'pods("v4") >= 1 and ready("v4") == 0'
if wait "$UPGRADE"; then fail "rollout to v4 should have failed (progressDeadlineSeconds)"; fi
pass "rollout to v4 failed as expected"
wait_for "still 100% v2 while v4 is stuck" 'pct("v2") >= 99 and ready("v4") == 0'
make -s undo >/dev/null
wait_for "v4 pod removed after undo" 'pods("v4") == 0'

# ------------------------------------------------------------------ blue/green
log "Blue/Green: green waits idle, switch is instant, rollback is instant"
make -s bg-start >/dev/null
wait_for "blue (v1) serves everything" 'pct("v1") >= 99'
make -s bg-deploy-green >/dev/null
wait_for "green (v2) ready but idle" 'ready("v2") == 4 and pct("v2") == 0'
make -s bg-switch >/dev/null
wait_for "switch: v2 serves everything within 30s" 'pct("v2") >= 99' 30
make -s bg-rollback >/dev/null
wait_for "rollback: v1 serves everything within 30s" 'pct("v1") >= 99' 30

# ------------------------------------------------------------------ canary
log "Canary: small share first, bad canary visible, abort, promote"
make -s canary-start >/dev/null
wait_for "stable v1 x9 serves everything" 'pct("v1") >= 99 and ready("v1") == 9'
make -s canary-deploy TO=v2 >/dev/null
wait_for "canary v2 gets a small share (1 of 10 pods)" 'ready("v2") == 1 and pct("v2") > 0 and pct("v2") < 30'
make -s canary-50 >/dev/null
wait_for "canary v2 gets about half" 'ready("v2") == 5 and pct("v2") > 30 and pct("v2") < 70'
make -s canary-abort >/dev/null
wait_for "abort: back to 100% v1" 'pct("v1") >= 99 and pods("v2") == 0'
make -s canary-deploy TO=v3 >/dev/null
wait_for "bad canary v3 shows errors" 'errs("v3") > 0'
make -s canary-abort >/dev/null
wait_for "abort: back to 100% v1" 'pct("v1") >= 99 and pods("v3") == 0'
make -s canary-deploy TO=v2 >/dev/null
make -s canary-promote TO=v2 >/dev/null
wait_for "promote: 100% v2, no v1 left" 'pct("v2") >= 99 and pods("v1") == 0'

make -s reset >/dev/null
log "ALL STRATEGIES PASSED"
