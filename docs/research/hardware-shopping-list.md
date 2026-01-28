# Hardware Shopping List for Testing

Hardware required for testing Harbor Satellite zero-trust features.

## Summary

| Kit | Cost | Use Case |
|-----|------|----------|
| Minimum | ~$200 | Phase 1-2, basic TPM testing |
| Recommended | ~$500 | Full testing, multiple architectures |
| Full Lab | ~$1500 | Fleet testing, stress testing |

---

## Minimum Test Lab (~$200)

For Phase 1-2 testing. Gets you started with real hardware.

| Item | Qty | Unit Price | Total | Notes |
|------|-----|------------|-------|-------|
| Raspberry Pi 4 Model B (4GB) | 1 | $55 | $55 | ARM edge device |
| SanDisk Extreme 64GB MicroSD | 1 | $12 | $12 | A2 rated for Pi |
| USB-C Power Supply (5V 3A) | 1 | $15 | $15 | Official Pi PSU |
| LetsTrust TPM for Pi | 1 | $25 | $25 | TPM 2.0, I2C interface |
| Ethernet cable (Cat6, 3ft) | 2 | $3 | $6 | For isolated network |
| **Total** | | | **$113** | |

Plus: Use existing laptop/desktop for Ground Control

### Where to Buy

- **Raspberry Pi**: [raspberrypi.com/products](https://www.raspberrypi.com/products/)
- **LetsTrust TPM**: [letstrust.de](https://letstrust.de/) or Amazon
- **MicroSD**: Amazon, Best Buy
- **Power Supply**: Official Pi store or Amazon

---

## Recommended Test Lab (~$500)

For comprehensive testing across ARM and x86.

| Item | Qty | Unit Price | Total | Notes |
|------|-----|------------|-------|-------|
| Raspberry Pi 4 Model B (4GB) | 2 | $55 | $110 | ARM testing |
| Raspberry Pi 5 (8GB) | 1 | $80 | $80 | Performance testing |
| SanDisk Extreme 64GB MicroSD | 3 | $12 | $36 | A2 rated |
| USB-C Power Supply (5V 3A) | 3 | $15 | $45 | Official Pi PSU |
| Intel NUC (refurbished i5) | 1 | $200 | $200 | x86 testing |
| LetsTrust TPM for Pi | 1 | $25 | $25 | For Pi |
| Infineon SLB9670 TPM | 1 | $20 | $20 | For NUC |
| TP-Link 5-port Gigabit Switch | 1 | $20 | $20 | Isolated network |
| Ethernet cables (Cat6) | 5 | $3 | $15 | Various lengths |
| **Total** | | | **$551** | |

### Intel NUC Options

- **Refurbished**: eBay, Amazon Renewed (~$200-300)
- **New NUC 12/13**: Intel store (~$400-600)
- **Alternative**: Any mini PC with TPM support

---

## Full Test Lab (~$1500)

For fleet testing, stress testing, and full E2E validation.

| Item | Qty | Unit Price | Total | Notes |
|------|-----|------------|-------|-------|
| Raspberry Pi 4 Model B (4GB) | 3 | $55 | $165 | ARM fleet |
| Raspberry Pi 5 (8GB) | 2 | $80 | $160 | Performance |
| Intel NUC (i5) | 2 | $300 | $600 | x86 fleet |
| SanDisk Extreme 128GB MicroSD | 5 | $18 | $90 | Larger storage |
| USB-C Power Supply (5V 3A) | 5 | $15 | $75 | Pi power |
| LetsTrust TPM for Pi | 2 | $25 | $50 | Pi TPM |
| Infineon SLB9670 TPM | 2 | $20 | $40 | NUC TPM |
| USB TPM (Infineon OPTIGA) | 1 | $50 | $50 | Portable |
| 8-port managed switch | 1 | $80 | $80 | VLAN support |
| Raspberry Pi Zero 2 W | 1 | $15 | $15 | Network sim |
| UPS (APC 600VA) | 1 | $80 | $80 | Power testing |
| Cables, rack, accessories | 1 | $50 | $50 | Various |
| **Total** | | | **$1455** | |

---

## Software Alternatives (Free)

For CI pipelines and initial development - no hardware needed.

### Software TPM (swtpm)

```bash
# Ubuntu/Debian
apt install swtpm swtpm-tools

# Start software TPM
mkdir /tmp/mytpm
swtpm socket --tpmstate dir=/tmp/mytpm \
  --ctrl type=unixio,path=/tmp/mytpm/swtpm-sock \
  --log level=20 --tpm2
```

- Works in Docker/VMs
- Good for unit tests
- NOT for attestation validation

### QEMU with TPM

```bash
# Run VM with emulated TPM
qemu-system-x86_64 \
  -chardev socket,id=chrtpm,path=/tmp/mytpm/swtpm-sock \
  -tpmdev emulator,id=tpm0,chardev=chrtpm \
  -device tpm-tis,tpmdev=tpm0
```

### Docker Compose for Integration Tests

```yaml
services:
  satellite:
    image: harbor-satellite:dev
    volumes:
      - /tmp/mytpm:/tmp/tpm

  ground-control:
    image: ground-control:dev

  harbor:
    image: goharbor/harbor-core:v2.9.0
```

---

## TPM Module Comparison

| Module | Platform | Interface | Price | Notes |
|--------|----------|-----------|-------|-------|
| LetsTrust TPM | Raspberry Pi | I2C | $25 | Best for Pi |
| Infineon SLB9670 | Intel NUC | LPC | $20 | Standard TPM |
| Infineon OPTIGA | Any (USB) | USB | $50 | Portable |
| Built-in TPM | Some NUCs | - | $0 | Check specs |

### Checking if Device Has TPM

```bash
# Linux
ls /dev/tpm*
cat /sys/class/tpm/tpm0/device/description

# Check TPM version
tpm2_getcap properties-fixed | grep TPM2_PT_FAMILY_INDICATOR
```

---

## Network Simulation Hardware

For testing intermittent connectivity, latency, packet loss.

### Option 1: Pi-based Network Simulator

Use a Raspberry Pi Zero 2 W as a bridge:

| Item | Price |
|------|-------|
| Raspberry Pi Zero 2 W | $15 |
| USB Ethernet adapter | $15 |
| MicroSD (32GB) | $8 |
| **Total** | **$38** |

Software: `tc` (traffic control) for latency/loss simulation

### Option 2: Managed Switch

Use VLAN and QoS features:

| Item | Price |
|------|-------|
| TP-Link TL-SG108E | $30 |
| Netgear GS308E | $40 |

### Option 3: Software Only

```bash
# Add 100ms latency
tc qdisc add dev eth0 root netem delay 100ms

# Add 10% packet loss
tc qdisc add dev eth0 root netem loss 10%

# Combine
tc qdisc add dev eth0 root netem delay 100ms loss 5%
```

---

## Power Failure Testing

### UPS Options

| Model | Price | Runtime | Notes |
|-------|-------|---------|-------|
| APC BE600M1 | $80 | 5-10 min | Basic |
| CyberPower CP1500 | $180 | 15-20 min | Better |

### Smart Plug (Controlled Power Cycling)

| Model | Price | Notes |
|-------|-------|-------|
| TP-Link Kasa | $15 | WiFi, API |
| Shelly Plug | $20 | API, scripting |

```bash
# Example: Power cycle via Kasa API
kasa --host 192.168.1.100 off
sleep 10
kasa --host 192.168.1.100 on
```

---

## Recommended Purchase Order

### Phase 1 (Start Here)

1. 1x Raspberry Pi 4 (4GB) + MicroSD + PSU
2. 1x LetsTrust TPM
3. Ethernet cables

**Cost: ~$100**

### Phase 2 (Add x86)

4. 1x Intel NUC (refurbished)
5. 1x Infineon SLB9670 TPM

**Additional: ~$220**

### Phase 3 (Fleet Testing)

6. 1x more Raspberry Pi 4
7. 1x Raspberry Pi 5
8. Network switch

**Additional: ~$180**

### Phase 4 (Stress Testing)

9. UPS
10. Additional devices as needed

**Additional: ~$100+**

---

## Vendor Links

- **Raspberry Pi**: https://www.raspberrypi.com/products/
- **LetsTrust TPM**: https://letstrust.de/
- **Intel NUC**: https://www.intel.com/content/www/us/en/products/details/nuc.html
- **Infineon TPM**: Amazon, Mouser, DigiKey
- **Network Gear**: Amazon, Newegg

---

## Related Documents

- [Test Strategy](./test-strategy.md)
- [Mock Interfaces](./mock-interfaces.md)
