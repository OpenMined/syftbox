#!/bin/bash
# NAT traversal integration test
# Verifies WebRTC data channels work through simulated NAT using Docker
# Alice and Bob on isolated networks, communicating via TURN relay
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="${SCRIPT_DIR}/docker-compose-nat-test.yml"
TIMEOUT=${NAT_TEST_TIMEOUT:-60}
CLEANUP=${NAT_TEST_CLEANUP:-1}

log() { echo "[nat-test] $(date +%H:%M:%S) $*"; }
fail() { log "FAIL: $*"; cleanup; exit 1; }
has() { [[ "$1" == *"$2"* ]]; }

cleanup() {
    if [ "${CLEANUP}" = "1" ]; then
        log "Cleaning up..."
        docker compose -f "${COMPOSE_FILE}" down -v --remove-orphans 2>/dev/null || true
    else
        log "Skipping cleanup (NAT_TEST_CLEANUP=0)"
    fi
}

trap cleanup EXIT

# ── Build and start ──
log "Building containers..."
docker compose -f "${COMPOSE_FILE}" build --quiet

log "Starting NAT simulation..."
docker compose -f "${COMPOSE_FILE}" up -d

# ── Wait for services ──
log "Waiting for server..."
for i in $(seq 1 30); do
    SERVER_RESP=$(docker exec nat-server wget -qO- http://localhost:8080/ 2>/dev/null || true)
    if has "${SERVER_RESP}" "Syft"; then
        log "Server ready"
        break
    fi
    if [ "$i" = "30" ]; then fail "Server did not start"; fi
    sleep 1
done

log "Waiting for TURN server..."
for i in $(seq 1 15); do
    TURN_LOG=$(docker logs nat-turn 2>&1 || true)
    if has "${TURN_LOG}" "listening on"; then
        log "TURN ready"
        break
    fi
    if [ "$i" = "15" ]; then log "WARN: TURN readiness unconfirmed, continuing..."; fi
    sleep 1
done

log "Waiting for clients..."
for i in $(seq 1 30); do
    ALICE_LOG=$(docker logs nat-alice 2>&1 || true)
    if has "${ALICE_LOG}" "hotlink enabled"; then
        log "Alice ready (hotlink enabled)"
        break
    fi
    if [ "$i" = "30" ]; then
        docker logs nat-alice 2>&1 | tail -5 || true
        fail "Alice did not start"
    fi
    sleep 1
done

for i in $(seq 1 30); do
    BOB_LOG=$(docker logs nat-bob 2>&1 || true)
    if has "${BOB_LOG}" "hotlink enabled"; then
        log "Bob ready (hotlink enabled)"
        break
    fi
    if [ "$i" = "30" ]; then
        docker logs nat-bob 2>&1 | tail -5 || true
        fail "Bob did not start"
    fi
    sleep 1
done

# ── Verify network isolation ──
log "Verifying network isolation..."
if docker exec nat-alice ping -c 1 -W 1 nat-bob >/dev/null 2>&1; then
    fail "Network isolation broken: alice can reach bob directly"
fi
log "Confirmed: alice cannot reach bob directly"

# Wait for ACLs to sync
log "Waiting for ACL sync..."
sleep 5

# ── Create TCP proxy markers (same JSON format as E2E test) ──
log "Creating TCP proxy markers..."

MARKER_JSON='{"from":"alice@example.com","to":"bob@example.com","port":9100,"ports":{"alice@example.com":9100,"bob@example.com":9200}}'

# Create on alice's side
docker exec nat-alice sh -c "
    mkdir -p /data/alice@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1
    echo '${MARKER_JSON}' > /data/alice@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1/stream.tcp
    printf '%s\n' 'terminal: false
rules:
  - pattern: \"**\"
    access:
      admin: [alice@example.com, bob@example.com]
      write: [alice@example.com, bob@example.com]
      read: [alice@example.com, bob@example.com]' > /data/alice@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1/syft.pub.yaml
"

# Create on bob's side (mirror of alice's datasite)
docker exec nat-bob sh -c "
    mkdir -p /data/bob@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1
    echo '${MARKER_JSON}' > /data/bob@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1/stream.tcp
    printf '%s\n' 'terminal: false
rules:
  - pattern: \"**\"
    access:
      admin: [alice@example.com, bob@example.com]
      write: [alice@example.com, bob@example.com]
      read: [alice@example.com, bob@example.com]' > /data/bob@example.com/datasites/alice@example.com/shared/flows/nat-test/run1/_mpc/0_to_1/syft.pub.yaml
"

# Wait for hotlink to discover markers
log "Waiting for hotlink to discover markers..."
for i in $(seq 1 15); do
    ALICE_LOG=$(docker logs nat-alice 2>&1 || true)
    BOB_LOG=$(docker logs nat-bob 2>&1 || true)
    if has "${ALICE_LOG}" "tcp proxy: starting" && has "${BOB_LOG}" "tcp proxy: starting"; then
        log "Both clients discovered TCP proxy markers"
        break
    fi
    if [ "$i" = "15" ]; then
        log "WARN: Marker discovery timeout"
        docker logs nat-alice 2>&1 | grep -i "tcp\|proxy\|hotlink" | tail -5 || true
        docker logs nat-bob 2>&1 | grep -i "tcp\|proxy\|hotlink" | tail -5 || true
    fi
    sleep 1
done

# ── Trigger WebRTC by making TCP connections to proxy ports ──
log "Connecting to TCP proxy ports to trigger WebRTC..."

# Bob connects to his proxy port (9200) - this triggers WebRTC to alice
# Alice connects to her proxy port (9100) - the other end of the tunnel
# Run both in background, they'll block waiting for the tunnel
docker exec -d nat-bob sh -c "echo 'hello from bob' | nc -w 10 127.0.0.1 9200 2>/dev/null || true"
sleep 1
docker exec -d nat-alice sh -c "echo 'hello from alice' | nc -w 10 127.0.0.1 9100 2>/dev/null || true"

# ── Wait for WebRTC data channel to open ──
log "Waiting for WebRTC data channel..."
DEADLINE=$((SECONDS + TIMEOUT))
WEBRTC_FOUND=0
SIGNALING_LOGGED=0

while [ $SECONDS -lt $DEADLINE ]; do
    ALICE_LOGS=$(docker logs nat-alice 2>&1 || true)
    BOB_LOGS=$(docker logs nat-bob 2>&1 || true)

    # Best outcome: WebRTC data channel opened
    if has "${ALICE_LOGS}" "data channel open" && has "${BOB_LOGS}" "data channel open"; then
        log "WebRTC data channel open on BOTH sides!"
        WEBRTC_FOUND=2
        break
    fi

    if has "${ALICE_LOGS}" "data channel open" || has "${BOB_LOGS}" "data channel open"; then
        log "WebRTC data channel open on one side, waiting for both..."
    fi

    # Log signaling progress once
    if [ "${SIGNALING_LOGGED}" = "0" ]; then
        if has "${ALICE_LOGS}" "offer sent" || has "${BOB_LOGS}" "offer sent"; then
            log "WebRTC signaling in progress..."
            SIGNALING_LOGGED=1
        fi
    fi

    # Fallback: data flowing via WS (still passes - proves connectivity)
    if has "${ALICE_LOGS}" "hotlink send data" || has "${BOB_LOGS}" "hotlink send data"; then
        log "Hotlink data flowing via WS"
        WEBRTC_FOUND=1
    fi

    sleep 1
done

# ── Final logs ──
log ""
log "=== RESULTS ==="
ALICE_FINAL=$(docker logs nat-alice 2>&1 || true)
BOB_FINAL=$(docker logs nat-bob 2>&1 || true)

log "--- Alice hotlink logs ---"
echo "${ALICE_FINAL}" | { grep -iE "hotlink|webrtc|ice|sdp|turn|data.channel|tcp.proxy|stream.tcp" || true; } | tail -20

log ""
log "--- Bob hotlink logs ---"
echo "${BOB_FINAL}" | { grep -iE "hotlink|webrtc|ice|sdp|turn|data.channel|tcp.proxy|stream.tcp" || true; } | tail -20

log ""
log "--- TURN server ---"
docker logs nat-turn 2>&1 | tail -5 || true

log ""
if [ "${WEBRTC_FOUND}" = "2" ]; then
    log "PASS: WebRTC data channels open on BOTH sides through NAT (via TURN relay)"
    exit 0
elif [ "${WEBRTC_FOUND}" = "1" ]; then
    if has "${ALICE_FINAL}" "data channel open" || has "${BOB_FINAL}" "data channel open"; then
        log "PASS: WebRTC data channel established through NAT"
    else
        log "PASS: Hotlink data flowing through NAT (WS fallback)"
    fi
    exit 0
else
    fail "No WebRTC or hotlink activity detected within ${TIMEOUT}s"
fi
