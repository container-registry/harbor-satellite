# Peer-copy MicroVM testbed

NixOS guests on a **Fedora host**. The satellite binary stays host-built;
MicroVMs are only the kill-a-node demo. Protocol is unchanged from
[docs/poc/peer-oci-copy.md](../../docs/poc/peer-oci-copy.md).

Hypervisor is **qemu** with **user-mode** networking (no TAP, no Cloud
Hypervisor). Guest gateway `10.0.2.2` is the Fedora host.

## Host smoke (do this first)

Do not start `sat-a` / `sat-b` yet. Confirm Nix, KVM, and one empty guest.

### 1. KVM on Fedora

```bash
ls -l /dev/kvm
groups   # should include kvm
```

If `/dev/kvm` is missing:

```bash
sudo dnf install -y qemu-kvm
```

If the node exists but you are not in `kvm`:

```bash
sudo usermod -aG kvm "$USER"
# log out and back in (or newgrp kvm)
```

### 2. Nix with flakes

If `nix --version` fails, install [Determinate Nix](https://github.com/DeterminateSystems/nix-installer):

```bash
curl -fsSL https://install.determinate.systems/nix | sh -s -- install
```

Open a **new** terminal so `nix` is on `PATH`.

**Do not `sudo nix`.** The store is owned by the daemon; sudo then hits
`Permission denied` on `/nix/var/nix/db/big-lock` or a different Nix. If a
normal `nix run` says `big-lock: Permission denied`, the daemon is down:

```bash
systemctl status nix-daemon --no-pager
sudo systemctl start nix-daemon
```

### 3. Git-track the flake (required)

Flakes copy only files Git knows about. Untracked `flake.nix` fails with
`is not tracked by Git`. From the **repository root** (no commit needed):

```bash
git add test/microvm/flake.nix test/microvm/README.md
```

### 4. Run the smoke MicroVM

From `test/microvm`, **without sudo**:

```bash
cd test/microvm
nix run .#smoke
```

First run downloads nixpkgs + microvm.nix and builds the guest (can take a
while). You should get a Linux console, autologged in as root.

Inside the guest:

```bash
hostname          # smoke
ping -c 2 10.0.2.2
```

`10.0.2.2` is the host. ICMP to the internet often fails on QEMU user-net;
that is not a failure.

Leave the VM with `poweroff` or Ctrl-A x (QEMU).

If the guest freezes at `Poking KASLR`, that is QEMU with **exactly 2048 MiB**
RAM ([microvm.nix#171](https://github.com/microvm-nix/microvm.nix/issues/171)).
sat-a / sat-b use `mem = 2049`. Ctrl-C the hung QEMU, then relaunch. Leave a
healthy sat-a running; only restart the VM that hung.

## sat-a (host-built binary)

Smoke proved QEMU user-net. Now one guest runs the **same** host-built
`bin/satellite` as the compose overlay, talking to Ground Control on the host
(`10.0.2.2:7080`). Do not start sat-b yet.

`--impure` is required so the flake can read `SATELLITE_ROOT` and 9p-mount `bin/` plus `test/microvm/secrets/`.

### 1. Ground Control on the host

Keep postgres + Ground Control. **Stop compose `satellite-a`** so host port 5000 is free:

```bash
cd /path/to/harbor-satellite
docker compose -f docker-compose.yml -f docker-compose.peer-poc.yml --profile peer-a stop satellite-a
curl -sf http://127.0.0.1:7080/ping   # pong
```

Use a **fresh unused** `TOKEN_A` (see [peer-oci-copy.md](../../docs/poc/peer-oci-copy.md)). Spent compose tokens will 400/429.

### 2. Host binary and secrets

```bash
# Fedora default go build is dynamically linked; NixOS cannot run it (stub-ld).
CGO_ENABLED=0 go build -o bin/satellite ./cmd/satellite
file bin/satellite   # statically linked

mkdir -p test/microvm/secrets
cp test/microvm/sat-a.env.example test/microvm/secrets/sat-a.env
# put TOKEN=... and HARBOR_REGISTRY_URL=... in secrets/sat-a.env, then save
```

`test/microvm/secrets/` is gitignored. Do not commit it.

### 3. Git-track new Nix files

```bash
git add test/microvm/flake.nix test/microvm/README.md \
  test/microvm/modules/common.nix test/microvm/modules/sat-a.nix \
  test/microvm/sat-a.env.example
```

### 4. Run sat-a

```bash
export SATELLITE_ROOT="$PWD"
cd test/microvm
nix run --impure .#sat-a
```

Inside the guest:

```bash
hostname   # sat-a
curl -sf http://10.0.2.2:7080/ping
systemctl status harbor-satellite --no-pager
journalctl -u harbor-satellite -e -n 50 --no-pager
```

Success:

```text
Replica proxy listening
Artifact replicated to OCI store
```

From the **host** (second terminal), after those logs:

```bash
curl -sv --max-time 3 http://127.0.0.1:5000/v2/
```

Expect HTTP 200 and `Docker-Distribution-API-Version: registry/2.0`.
`curl -sI` sends HEAD; `/v2/` only allows GET, so HEAD is 405.

If `harbor-satellite` fails immediately: 9p path (`ls /mnt/satellite-bin/satellite`), empty `TOKEN`, or GC not reachable from the guest.

## sat-b (peer pull) and kill-A

Leave **sat-a running**. B pulls A's replica proxy through the host:
`PEER_URLS=http://10.0.2.2:5000` (QEMU user-net gateway → host 5000 → sat-a).

Same Ground Control **group** as A (`peer-group` / `peer-poc`). New satellite name
(Harbor robots cannot reuse `sat-a-vm16`). Use a **fresh ZTR token**.

### 1. Token and env

```bash
# login session, then:
curl -s http://127.0.0.1:7080/api/satellites \
  -H 'Content-Type: application/json' \
  -H "$AUTH" \
  -d '{
    "name": "sat-b-vm16",
    "groups": ["peer-group"],
    "config_name": "peer-poc"
  }'
```

If Harbor says robot name already present, change the name. Then:

```bash
cp test/microvm/sat-b.env.example test/microvm/secrets/sat-b.env
# TOKEN=... and HARBOR_REGISTRY_URL=https://demo.goharbor.io
```

Stop compose `satellite-b` if it is using host 5001. Keep `bin/satellite` **statically**
linked (`CGO_ENABLED=0`).

```bash
git add test/microvm/modules/sat-b.nix test/microvm/sat-b.env.example \
  test/microvm/flake.nix test/microvm/README.md
```

### 2. Run sat-b (second terminal)

sat-a stays in the first terminal. From the repo root:

```bash
export SATELLITE_ROOT="$PWD"
cd test/microvm
nix run --impure .#sat-b
```

Inside sat-b:

```bash
hostname   # sat-b
curl -sf http://10.0.2.2:7080/ping
curl -sv http://10.0.2.2:5000/v2/   # A's proxy via the host
journalctl -u harbor-satellite -e -n 80 --no-pager
```

Success:

```text
Replica proxy listening
Artifact copied from peer
```

`peer` should be `http://10.0.2.2:5000`. Dest ref stays Harbor
(`demo.goharbor.io/test-proj/test-image:latest`), not the peer host.
`falling back to Harbor` here means A was empty or unreachable.

### 3. Kill-A fallback (keep sat-b's ZTR config)

Do **not** delete `sat-b-data.img` (that burns robot creds). On **sat-b**:

```bash
systemctl stop harbor-satellite
rm -rf /var/lib/satellite/oci
```

On **sat-a**: `poweroff` (or quit that QEMU). Confirm host `:5000` is dead:

```bash
curl -sv --max-time 2 http://127.0.0.1:5000/v2/
```

On **sat-b**:

```bash
systemctl start harbor-satellite
journalctl -u harbor-satellite -f
```

Expect `No peer has artifact, falling back to Harbor` then
`Artifact replicated to OCI store`.

## After a host crash

QEMU guests die with the host. Re-run the VMs. Do not `git add` secrets.

1. Ground Control: `curl -sf http://127.0.0.1:7080/ping` — if empty, `up -d postgres ground-control`.
2. Static binary: `file bin/satellite` must say statically linked; if not, `CGO_ENABLED=0 go build -o bin/satellite ./cmd/satellite`.
3. **sat-a first** (it must hold the image before B starts):

```bash
export SATELLITE_ROOT="$PWD"
cd test/microvm
nix run --impure .#sat-a
```

If `sat-a-data.img` survived, ZTR is already done (robot creds on the volume).
You should see `Replica proxy listening` and `already up-to-date` or a Harbor
replicate. **Do not** put a spent token back in `secrets/sat-a.env` and expect
ZTR to work; if the volume is gone, mint a **new** satellite name (Harbor robot
names cannot be reused).

Host check: `curl -sv --max-time 3 http://127.0.0.1:5000/v2/` → 200.

4. **sat-b** in a second terminal, same `SATELLITE_ROOT`, `nix run --impure .#sat-b`.
   Fresh `TOKEN` in `secrets/sat-b.env` unless `sat-b-data.img` already completed ZTR.
5. Kill-A demo only after B logs `Artifact copied from peer`.



