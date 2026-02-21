#!/bin/bash
# Find Raspberry Pi devices on the local network
# Ping sweeps the subnet, then filters for known Pi MAC prefixes

set -euo pipefail

IFACE="${1:-}"

# Auto-detect interface if not provided
if [ -z "$IFACE" ]; then
    IFACE=$(ip -4 route show default | awk '{print $5}' | head -1)
fi

if [ -z "$IFACE" ]; then
    echo "Error: could not detect network interface. Pass it as an argument: $0 <iface>"
    exit 1
fi

GATEWAY=$(ip -4 route show default dev "$IFACE" | awk '{print $3}' | head -1)
SUBNET_PREFIX="${GATEWAY%.*}"
MY_IP=$(ip -4 addr show "$IFACE" 2>/dev/null | grep -oP 'inet \K[0-9.]+' | head -1)

# Known Raspberry Pi Foundation OUI prefixes
PI_PREFIXES="b8:27:eb d8:3a:dd dc:a6:32 e4:5f:01 2c:cf:67"

echo "Interface : $IFACE"
echo "Gateway   : $GATEWAY"
echo "Local IP  : $MY_IP"
echo "Scanning ${SUBNET_PREFIX}.0/24 ..."
echo ""

# Parallel ping sweep
for i in $(seq 1 254); do
    ping -c 1 -W 1 "${SUBNET_PREFIX}.${i}" > /dev/null 2>&1 &
done
wait

# Collect results
found=0
echo "Live devices:"
echo "----------------------------------------------"
printf "  %-16s %-19s %s\n" "IP" "MAC" "NOTE"
echo "----------------------------------------------"

arp -an | grep "$IFACE" | grep -v incomplete | while read -r line; do
    ip=$(echo "$line" | grep -oP '\(\K[0-9.]+')
    mac=$(echo "$line" | grep -oP '([0-9a-f]{2}:){5}[0-9a-f]{2}')
    [ -z "$mac" ] && continue
    [ "$ip" = "$GATEWAY" ] && continue
    [ "$ip" = "$MY_IP" ] && continue

    note=""
    mac_lower=$(echo "$mac" | tr '[:upper:]' '[:lower:]')
    for prefix in $PI_PREFIXES; do
        if [[ "$mac_lower" == "$prefix"* ]]; then
            note="<-- Raspberry Pi"
            break
        fi
    done

    printf "  %-16s %-19s %s\n" "$ip" "$mac" "$note"
done

echo ""
echo "Tip: SSH into a Pi with:  ssh pi@<IP>"
