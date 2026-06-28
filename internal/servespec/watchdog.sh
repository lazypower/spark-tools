#!/bin/sh
# vLLM engine-wedge watchdog.
#
# Detects the deadlock where requests are "running" but NO tokens come out
# (the failure mode seen 2026-06-24: GPU spinning, /health still 200, engine
# logger silent). /health does NOT catch this, so we watch for lack of
# forward progress instead and restart the vllm container when it's wedged.
#
# Boot-safety (so it never goes HAM during a load/reload):
#   * INITIAL_PAUSE before it looks at anything
#   * only evaluates while /health is 200 (boot/reload => /health fails => paused)
#   * long STALL_WINDOW of *no token progress* required before acting
#   * after it restarts, it waits for readiness before resuming
#   * restart-storm guard: at most MAX_RESTARTS within RESTART_WINDOW, then it
#     gives up and just alerts (a human should look).

VLLM_URL="${VLLM_URL:-http://vllm:8000}"
POLL_INTERVAL="${POLL_INTERVAL:-30}"      # seconds between checks
STALL_WINDOW="${STALL_WINDOW:-240}"       # no-token-progress seconds (with reqs running) => wedged
STARTUP_GRACE="${STARTUP_GRACE:-900}"     # max seconds to wait for readiness after boot/restart
INITIAL_PAUSE="${INITIAL_PAUSE:-90}"      # ignore the server entirely for this long at startup
MAX_RESTARTS="${MAX_RESTARTS:-3}"         # auto-restarts allowed within RESTART_WINDOW
RESTART_WINDOW="${RESTART_WINDOW:-3600}"  # rolling window (s) for the storm guard
HEARTBEAT="${HEARTBEAT:-600}"             # log an "alive" line this often (s)
COMPOSE_PROJECT="${COMPOSE_PROJECT:-vllm}"
COMPOSE_SERVICE="${COMPOSE_SERVICE:-vllm}"

log() { echo "[watchdog $(date -u '+%Y-%m-%dT%H:%M:%SZ')] $*"; }

healthy() { wget -qO- -T 5 "$VLLM_URL/health" >/dev/null 2>&1; }

# echoes "<running> <prompt+generation tokens>"; always two integers
snapshot() {
  wget -qO- -T 5 "$VLLM_URL/metrics" 2>/dev/null | awk '
    /^vllm:num_requests_running\{/    { r=$NF }
    /^vllm:prompt_tokens_total\{/     { p=$NF }
    /^vllm:generation_tokens_total\{/ { g=$NF }
    END { printf "%d %d", (r+0), ((p+0)+(g+0)) }'
}

target_cid() {
  docker ps -q \
    -f "label=com.docker.compose.project=$COMPOSE_PROJECT" \
    -f "label=com.docker.compose.service=$COMPOSE_SERVICE" | head -n1
}

wait_ready() {
  waited=0
  while [ "$waited" -lt "$STARTUP_GRACE" ]; do
    if healthy; then log "server healthy."; return 0; fi
    sleep "$POLL_INTERVAL"; waited=$((waited + POLL_INTERVAL))
  done
  log "WARN: not healthy within ${STARTUP_GRACE}s; resuming monitoring anyway."
  return 1
}

log "starting: url=$VLLM_URL poll=${POLL_INTERVAL}s stall_window=${STALL_WINDOW}s grace=${STARTUP_GRACE}s guard=${MAX_RESTARTS}/${RESTART_WINDOW}s"
sleep "$INITIAL_PAUSE"
wait_ready

last_progress=""
stall=0
restart_count=0
window_start=$(date +%s)
hb=0

while true; do
  sleep "$POLL_INTERVAL"

  if ! healthy; then
    # booting, reloading, or down: never act here. Reloads are owned by the
    # restart handler below, which waits for readiness before returning.
    [ "$stall" -gt 0 ] && log "health failing; pausing wedge detection, resetting stall."
    stall=0; last_progress=""
    continue
  fi

  snap="$(snapshot)"
  running="${snap%% *}"; progress="${snap##* }"
  [ -z "$running" ] && running=0
  [ -z "$progress" ] && progress=0

  hb=$((hb + POLL_INTERVAL))
  if [ "$hb" -ge "$HEARTBEAT" ]; then
    log "alive: running=$running tokens=$progress stall=${stall}s restarts=${restart_count}"
    hb=0
  fi

  # forward progress = the prompt+generation token counter advancing.
  # Only a stall *with requests in flight* counts as a wedge.
  if [ "$running" -gt 0 ] && [ "$progress" = "$last_progress" ]; then
    stall=$((stall + POLL_INTERVAL))
    log "no token progress: running=$running tokens=$progress stall=${stall}s/${STALL_WINDOW}s"
  else
    stall=0
  fi
  last_progress="$progress"

  if [ "$stall" -ge "$STALL_WINDOW" ]; then
    now=$(date +%s)
    [ $((now - window_start)) -ge "$RESTART_WINDOW" ] && { restart_count=0; window_start=$now; }

    if [ "$restart_count" -ge "$MAX_RESTARTS" ]; then
      log "CRITICAL: wedged again but already restarted ${restart_count}x within ${RESTART_WINDOW}s. NOT restarting — needs a human. Still watching."
      stall=0; last_progress=""
      continue
    fi

    restart_count=$((restart_count + 1))
    cid="$(target_cid)"
    log "WEDGED: running=$running, token counter flat at $progress for ${stall}s. Restart #${restart_count}/${MAX_RESTARTS} (cid=${cid:-NOT-FOUND})."
    if [ -n "$cid" ]; then
      log "---- vllm log tail (pre-restart) ----"
      docker logs --tail 20 "$cid" 2>&1 | sed 's/^/[vllm] /'
      log "---- end tail ----"
      if docker restart "$cid" >/dev/null 2>&1; then log "restart issued."; else log "ERROR: docker restart failed."; fi
    else
      log "ERROR: vllm container not found by compose label ($COMPOSE_PROJECT/$COMPOSE_SERVICE)."
    fi
    stall=0; last_progress=""
    sleep "$INITIAL_PAUSE"
    wait_ready
  fi
done
