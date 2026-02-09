#!/bin/bash
set -euo pipefail

TURN_USER="${1:?Usage: setup-coturn.sh <turn_user> <turn_pass>}"
TURN_PASS="${2:?Usage: setup-coturn.sh <turn_user> <turn_pass>}"

EXTERNAL_IP=$(curl -s --max-time 5 ifconfig.me || curl -s --max-time 5 icanhazip.com || echo "")
if [ -z "$EXTERNAL_IP" ]; then
    echo "ERROR: Could not detect external IP"
    exit 1
fi
echo "Detected external IP: $EXTERNAL_IP"

if ! command -v turnserver &>/dev/null; then
    echo "Installing coturn..."
    apt-get update -qq
    apt-get install -y -qq coturn
else
    echo "coturn already installed: $(turnserver --version 2>&1 | head -1)"
fi

# Disable the default coturn service that Debian enables (uses /etc/default/coturn)
if [ -f /etc/default/coturn ]; then
    sed -i 's/^#*TURNSERVER_ENABLED=.*/TURNSERVER_ENABLED=1/' /etc/default/coturn
fi

echo "Configuring coturn..."
if [ -f /tmp/turnserver.conf ]; then
    sed \
        -e "s/TURN_USER_PLACEHOLDER/$TURN_USER/g" \
        -e "s/TURN_PASS_PLACEHOLDER/$TURN_PASS/g" \
        -e "s/EXTERNAL_IP_PLACEHOLDER/$EXTERNAL_IP/g" \
        /tmp/turnserver.conf > /etc/turnserver.conf
    chmod 640 /etc/turnserver.conf
    echo "Wrote /etc/turnserver.conf"
else
    echo "ERROR: /tmp/turnserver.conf not found"
    exit 1
fi

if [ -f /tmp/coturn.service ]; then
    cp /tmp/coturn.service /etc/systemd/system/coturn.service
    echo "Installed coturn.service"
fi

systemctl daemon-reload
systemctl enable coturn
systemctl restart coturn
sleep 2

if systemctl is-active --quiet coturn; then
    echo "coturn is running"
else
    echo "ERROR: coturn failed to start"
    journalctl -u coturn --no-pager -n 20
    exit 1
fi

SYFTBOX_SERVICE="/etc/systemd/system/syftbox.service"
if [ -f "$SYFTBOX_SERVICE" ]; then
    echo "Updating syftbox.service with TURN env vars..."

    # Remove old TURN env lines if present
    sed -i '/^Environment=SYFTBOX_HOTLINK_ICE_SERVERS=/d' "$SYFTBOX_SERVICE"
    sed -i '/^Environment=SYFTBOX_HOTLINK_TURN_USER=/d' "$SYFTBOX_SERVICE"
    sed -i '/^Environment=SYFTBOX_HOTLINK_TURN_PASS=/d' "$SYFTBOX_SERVICE"

    # Add TURN env vars after the existing Environment= line
    sed -i "/^\[Service\]/a Environment=SYFTBOX_HOTLINK_ICE_SERVERS=turn:${EXTERNAL_IP}:3478?transport=udp\nEnvironment=SYFTBOX_HOTLINK_TURN_USER=${TURN_USER}\nEnvironment=SYFTBOX_HOTLINK_TURN_PASS=${TURN_PASS}" "$SYFTBOX_SERVICE"

    systemctl daemon-reload
    systemctl restart syftbox
    sleep 2

    if systemctl is-active --quiet syftbox; then
        echo "syftbox restarted with TURN config"
    else
        echo "WARNING: syftbox failed to restart"
        journalctl -u syftbox --no-pager -n 10
    fi
else
    echo "WARNING: $SYFTBOX_SERVICE not found, skipping syftbox TURN config"
fi

echo ""
echo "=== TURN server deployment complete ==="
echo "  External IP: $EXTERNAL_IP"
echo "  Listening:   UDP 3478"
echo "  Relay ports: UDP 49152-49200"
echo "  User:        $TURN_USER"
echo "  ICE URL:     turn:${EXTERNAL_IP}:3478?transport=udp"

# Health check
if command -v turnutils_uclient &>/dev/null; then
    echo ""
    echo "Running health check..."
    if timeout 10 turnutils_uclient -T -p 3478 127.0.0.1 -u "$TURN_USER" -w "$TURN_PASS" 2>&1 | grep -q "Total"; then
        echo "Health check PASSED"
    else
        echo "Health check inconclusive (turnutils_uclient output may vary)"
    fi
fi

# Cleanup temp files
rm -f /tmp/turnserver.conf /tmp/coturn.service /tmp/setup-coturn.sh
