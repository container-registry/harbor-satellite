# PARSEC Integration — Design Notes & Draft

> Working notes for [harbor-satellite#327](https://github.com/container-registry/harbor-satellite/issues/327).
> Covers design analysis, architectural decisions, skeleton implementation, and open questions.

---

## Problem Statement

Harbor Satellite operates on edge devices that are frequently physically accessible and deployed
in untrusted environments. The current security model is entirely software-based:

- **Software keys can be extracted.** An attacker with physical access can read credentials from
  disk or memory.
- **Device identity is not hardware-attested.** Software fingerprints (machine-id + MAC + disk
  serial) can be spoofed or copied to a different machine.
- **Config encryption has no hardware binding.** The encrypted `config.json` can be copied to
  another device and decrypted, because the AES key is derived from software-accessible values.
- **Ground Control cannot verify device authenticity.** Without hardware attestation, Ground
  Control must trust software-reported identity.

---

## Proposed Solution: CNCF PARSEC

[PARSEC](https://parsec.community/) (Platform AbstRaction for SECurity) provides a single Unix
socket API over hardware security backends: TPM 2.0, ARM TrustZone/CryptoCell, Intel SGX,
PKCS#11 HSMs. The satellite code has no compile-time dependency on any specific hardware library.

### Core properties

- Private keys are **non-exportable** — they are generated inside the secure element and never
  leave it.
- Config sealing uses a **hardware-resident symmetric key** — encrypted config cannot be decrypted
  on a different device even if an attacker has the ciphertext.
- The PARSEC daemon is **external infrastructure** (like SPIRE), not a bundled library. The
  host OS is responsible for running it.

---

## Design Analysis

### Relationship to ADR-0005 (SPIFFE/SPIRE Identity)

PARSEC and SPIRE are **complementary, not competing**:

| Layer | Component | Role |
|---|---|---|
| Key generation & storage | PARSEC | Generates hardware-resident keys; private key never leaves secure element |
| Node attestation | SPIRE `tpm_devid` plugin | Uses PARSEC-backed key to attest the SPIRE agent to the SPIRE server |
| Workload identity | SPIRE | Issues X.509 SVIDs after attestation succeeds |
| mTLS & ZTR | Existing SPIFFE client | Unchanged — receives SVID and uses it for mTLS with Ground Control |

PARSEC plugs into SPIRE at the **node attestation layer**. The existing `internal/spiffe/client.go`
SVID delivery flow is unaffected. This maps directly onto ADR-0005's explicit roadmap:

- **Phase 2** (planned): TPM-based node attestation via SPIRE `tpm_devid` plugin — PARSEC enables this
- **Phase 3** (planned): HSM identity store for satellite certs/credentials — PARSEC enables this

### What the bootstrapping flow proposal gets right

The `PARSEC Integration & Zero-Trust Bootstrapping Flow.md` document correctly describes the
6-step sequence:

1. Detect PARSEC daemon; fall back gracefully if absent
2. Check for existing key pair; generate non-exportable key on first boot
3. Generate CSR + attestation quote; deliver to Ground Control (direct or air-gap)
4. Ground Control verifies hardware proof; issues SPIFFE SVID
5. Satellite uses SVID for mTLS; fetches Robot Account credentials; PARSEC unseals config
6. Heartbeat → credential rotation over mTLS tunnel

Steps 1–2 and 5–6 are clean. Steps 3–4 need clarification:

### Key design tension: who issues the SVID?

The bootstrapping flow says *"Ground Control issues a SPIFFE SVID"*. This is correct but needs
precision: it is the **embedded SPIRE Server within Ground Control** that issues the SVID via its
`tpm_devid` attestation plugin — not a custom Ground Control endpoint. Ground Control must not
reimplement TPM attestation validation (verifying endorsement key certificate chains, PCR
baselines). SPIRE already does this correctly.

### Software fallback vs. fail-hard

The bootstrapping flow proposes a `FileKeyProvider` fallback when PARSEC is absent. ADR-0005
establishes a "fail-hard, no fallback" rule when a secure mode is enabled. These are in tension.

**Resolution**: follow ADR-0005's philosophy. When `--parsec-enabled` is set, the satellite halts
if the daemon is unreachable. The fallback for development/VM environments is simply not setting
`--parsec-enabled` — the existing AES software path remains the default.

### The Go TLS incompatibility

Standard Go TLS (`tls.LoadX509KeyPair`) expects an exportable private key. PARSEC keys are
non-exportable by design. The fix is to implement `crypto.Signer` backed by PARSEC:

```go
type Signer struct {
    client  *parsecclient.BasicClient
    keyName string
    pub     crypto.PublicKey  // exported DER public key only
}

func (s *Signer) Public() crypto.PublicKey { return s.pub }
func (s *Signer) Sign(_ io.Reader, digest []byte, _ crypto.SignerOpts) ([]byte, error) {
    return s.client.PsaSignHash(s.keyName, digest, ecdsaP256Sha256Alg)
}

// Wire into standard TLS — private key stays in hardware
cert := tls.Certificate{
    Certificate: [][]byte{derCert},
    PrivateKey:  parsecSigner,
}
```

Standard Go TLS calls `Sign()` transparently. No change to the TLS layer's callers.

---

## Skeleton Implementation

### Package structure

```
internal/parsec/
  config.go          # Config struct, DefaultConfig(), key name constants    [no tag]
  detect.go          # Detect(socketPath) bool, MustDetect() error           [parsec]
  detect_stub.go     # returns ErrParsecNotAvailable                         [!parsec]
  signer.go          # Signer implementing crypto.Signer                     [parsec]
  provider.go        # KeyProvider implementing crypto.Provider; KeyRef type [parsec]
  provider_stub.go   # no-op stubs, same shape as spiffe/client_stub.go      [!parsec]
```

All files under `internal/parsec/` follow the same build tag pattern as `internal/spiffe/`:
`//go:build parsec` for real implementations, `//go:build !parsec` for stubs. Default builds
compile to zero PARSEC dependency.

### Build tags

| Tag | Crypto backend | Identity |
|---|---|---|
| *(default)* | AES-256-GCM, software device fingerprint | SPIFFE/SPIRE or token |
| `nospiffe` | no-op (dev only) | token only |
| `parsec` | AES-256-GCM + PARSEC AEAD, hardware-bound | PARSEC + SPIFFE/SPIRE |

The `parsec` tag is independent of `nospiffe`. A `parsec` build still supports SPIFFE — in fact,
PARSEC is most useful when SPIFFE is also enabled, since it provides the hardware root for SPIRE
node attestation.

### Key design decisions

**`KeyRef` as `crypto.PrivateKey`**

The existing `crypto.Provider` interface has `Sign(data []byte, key crypto.PrivateKey)`.
`crypto.PrivateKey` is `any`. For PARSEC, private keys never leave hardware — the "private key"
passed around is a name reference:

```go
// KeyRef satisfies crypto.PrivateKey (which is `any`).
// It is a name reference, not key material.
type KeyRef struct{ Name string }
```

`ParsecKeyProvider.Sign()` type-asserts the key to `KeyRef` and uses the name to call
`PsaSignHash`. This satisfies the existing interface without any changes to callers.

**Fixed hardware key names**

Two hardware-resident keys are managed, idempotently, on first boot:
- `satellite-identity-key` — ECDSA P-256 signing key (CSR, mTLS, SPIRE attestation)
- `satellite-config-seal-key` — AES-256-GCM symmetric key (config sealing at rest)

Because the keys live in hardware, they are device-specific by nature. There is no need to derive
key names from software fingerprints.

**`DeriveKey` and `Hash` remain software**

Key derivation (Argon2id) and hashing (SHA-256) do not need hardware involvement. Only
non-exportability matters — and that is provided by the storage of the key itself.

**Idempotent key provisioning**

`NewKeyProvider()` calls `ListKeys()` on the PARSEC daemon and only generates keys if they are
absent. On all subsequent boots, the existing hardware keys are used. This means first-boot key
generation happens exactly once per device.

### New CLI flags

```
--parsec-enabled           bool    Enable hardware-backed identity (requires parsec build + daemon)
                                   Env: PARSEC_ENABLED=true
--parsec-socket <path>     string  PARSEC daemon socket path
                                   Default: /run/parsec/parsec.sock
                                   Env: PARSEC_SOCKET=<path>
```

When `--parsec-enabled` is set, `MustDetect()` runs at startup and halts the satellite with a
clear error if the daemon is unreachable — identical behaviour to SPIFFE when the SPIRE agent
socket is absent.

### Changes to existing files

| File | Change |
|---|---|
| `cmd/main.go` | `ParsecEnabled`/`ParsecSocketPath` in `SatelliteOptions`; new flags; `MustDetect()` startup check |
| `go.mod` | `replace` directive for local `parsec-client-go`; `require` entry |
| `pkg/config/manager.go` | *(pending)* `NewConfigManager` needs a path to accept a `crypto.Provider` so `ParsecKeyProvider` can be injected |
| `internal/tls/config.go` | *(pending)* `LoadCertificate` needs a PARSEC path using `crypto.Signer` instead of `tls.LoadX509KeyPair` |

---

## Open Questions & Remaining Work

### Must be done before this is usable

1. **Wire `KeyProvider` into `pkg/config/manager.go`**
   `NewConfigManager` currently hard-codes `crypto.NewAESProvider()`. It needs a constructor
   variant that accepts a `crypto.Provider` so the caller (in `cmd/main.go`) can pass a
   `ParsecKeyProvider` when `--parsec-enabled` is set.

2. **`internal/tls/` compatibility**
   `LoadCertificate` must support the `crypto.Signer` path so PARSEC-backed keys work with the
   existing TLS layer without using `tls.LoadX509KeyPair`.

3. **Verify `DefaultKeyAttribute().AeadKey()` in `parsec-client-go`**
   The skeleton calls this for the config seal key. If it does not exist, the `KeyAttributes`
   struct must be constructed directly. Needs a quick check against the local library.

### Phase 2: SPIRE `tpm_devid` integration

Once the satellite has a PARSEC-backed signing key, the next step is to use it as the private
key for SPIRE's `tpm_devid` node attestation plugin. This makes Ground Control's trust in the
satellite cryptographically hardware-rooted — a satellite cannot impersonate another because the
attesting key cannot be extracted.

This is explicitly called out as Phase 2 in ADR-0005 and is the piece that realises the full
zero-trust bootstrapping flow.

### Phase 3: Air-gap enrollment

The bootstrapping flow document proposes a USB/file-based path for air-gapped satellites (CSR +
attestation quote exported to file, admin carries it to Ground Control manually). This is a
valuable use case for industrial and disconnected deployments but is a separate feature from the
core PARSEC integration and should be designed independently.

### Tests needed

- Unit tests for `KeyProvider` using a mock PARSEC client (same pattern as `internal/crypto/mock.go`)
- E2E test similar to `TestSpiffeJoinTokenE2E` that exercises the full flow with a real PARSEC
  daemon in CI (the `parsec-client-go` repo has a Docker-based test harness that can be reused)

---

## File References

| File | Description |
|---|---|
| `internal/parsec/` | New package — skeleton implementation |
| `harbor-satellite/docs/decisions/0007-security-plugins-parsec.md` | ADR for this decision |
| `harbor-satellite/docs/decisions/0005-spiffe-identity-and-security.md` | ADR this builds on |
| `PARSEC Integration & Zero-Trust Bootstrapping Flow.md` | Original 6-step flow proposal |
| `PARSEC-HARBOR-INTEGRATION-PROPOSAL.md` | Broader Harbor Core proposal (separate scope) |
| `parsec-client-go/` | Local checkout of the Go PARSEC client library |
