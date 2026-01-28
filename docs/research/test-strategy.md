# Harbor Satellite Test Strategy

Test-first approach for implementing zero-trust features.

## Test Pyramid

```
                       ┌─────────┐
                       │  E2E    │  Real hardware, real network
                       │  Tests  │  (requires physical devices)
                      ┌┴─────────┴┐
                      │Integration│  Docker/VMs, mocked external
                      │   Tests   │  services, real crypto
                     ┌┴───────────┴┐
                     │    Unit     │  Mocked everything, fast,
                     │    Tests    │  runs in CI
                    ┌┴─────────────┴┐
                    │   Contract    │  API contracts, schema
                    │    Tests      │  validation
                   └───────────────────┘
```

## Test Environments

### CI Environment (GitHub Actions)
- Unit tests with mocks
- Integration tests with Docker
- Contract tests
- No real hardware, all simulated
- Software TPM (swtpm)

### Local Dev Environment
- Docker Compose stack
- Mock Ground Control
- Mock Harbor registry
- Software TPM simulator (swtpm)

### Hardware Test Lab
- Real Raspberry Pis
- Real TPM modules
- Real network (with simulation)
- Real Ground Control + Harbor
- Power failure testing
- Network failure testing

---

## Phase 1 Tests: Foundation

### Unit Tests (Mocked)

| Test Area | What to Test | Mock Required |
|-----------|--------------|---------------|
| Config Encryption | Encrypt/decrypt config with derived key | CryptoProvider |
| Key Derivation | Derive key from device fingerprint | DeviceIdentity |
| TLS Setup | Certificate loading, validation | Filesystem |
| Join Token | Token parsing, validation, single-use | GroundControl |
| Device Fingerprint | Consistent fingerprint generation | DeviceIdentity |

### Test Cases - Config Encryption

```
TestConfigEncryption:
  - encrypt_config_success
  - encrypt_config_empty_key_fails
  - decrypt_config_success
  - decrypt_config_wrong_key_fails
  - decrypt_config_corrupted_fails
  - encrypt_decrypt_roundtrip
  - encrypted_file_not_readable_as_plaintext
  - re_encryption_with_new_key
```

### Test Cases - Key Derivation

```
TestKeyDerivation:
  - derive_key_from_fingerprint
  - derive_key_deterministic (same input = same key)
  - derive_key_different_inputs_different_keys
  - derive_key_minimum_entropy_check
```

### Test Cases - Join Token Bootstrap

```
TestJoinTokenBootstrap:
  - valid_token_accepted
  - expired_token_rejected
  - malformed_token_rejected
  - token_single_use (second use fails)
  - token_rate_limiting
  - token_with_wrong_ground_control_rejected
```

### Test Cases - Device Fingerprint

```
TestDeviceFingerprint:
  - fingerprint_generation_consistent
  - fingerprint_survives_reboot (mocked)
  - fingerprint_changes_on_hardware_change
  - fingerprint_components_all_present
  - fingerprint_fallback_when_component_unavailable
```

---

## Phase 2 Tests: Zero-Trust Identity

### Unit Tests (Mocked)

| Test Area | What to Test | Mock Required |
|-----------|--------------|---------------|
| mTLS Handshake | Client cert auth, server validation | TLS stack |
| Certificate Rotation | Auto-rotation before expiry | CryptoProvider, Clock |
| Token Lifecycle | Issue, validate, rotate, revoke | TokenStore |
| Audit Logging | All operations logged | Logger |

### Test Cases - mTLS

```
TestMTLS:
  - mtls_handshake_success
  - mtls_handshake_no_client_cert_fails
  - mtls_handshake_expired_cert_fails
  - mtls_handshake_revoked_cert_fails
  - mtls_handshake_wrong_ca_fails
  - mtls_certificate_pinning
  - mtls_fallback_disabled (no downgrade to plain TLS)
```

### Test Cases - Credential Rotation

```
TestCredentialRotation:
  - rotation_before_expiry
  - rotation_triggered_at_threshold (e.g., 80% lifetime)
  - rotation_failure_retry
  - rotation_failure_graceful_degradation
  - old_credential_invalidated_after_rotation
  - rotation_during_offline (queued)
```

### Test Cases - Audit Logging

```
TestAuditLogging:
  - registration_logged
  - state_sync_logged
  - credential_rotation_logged
  - authentication_failure_logged
  - log_contains_satellite_id
  - log_contains_timestamp
  - log_tamper_detection (optional)
```

---

## Phase 3 Tests: SPIFFE/SPIRE

### Unit Tests (Mocked)

| Test Area | What to Test | Mock Required |
|-----------|--------------|---------------|
| SPIRE Agent Connection | Socket connection, API calls | SPIREClient |
| SVID Lifecycle | Fetch, validate, rotate | SPIREClient |
| Attestation | Node and workload attestation | SPIREClient, TPMDevice |
| Trust Bundle | Fetch, update, validate | SPIREClient |

### Test Cases - SPIRE Integration

```
TestSPIREIntegration:
  - connect_to_spire_agent
  - fetch_svid_success
  - fetch_svid_agent_unavailable_fallback
  - svid_validation_success
  - svid_validation_expired_fails
  - svid_rotation_automatic
  - trust_bundle_update
  - trust_bundle_validation
```

### Test Cases - Node Attestation

```
TestNodeAttestation:
  - join_token_attestation
  - tpm_attestation (requires real/mock TPM)
  - aws_iid_attestation (mocked AWS metadata)
  - gcp_attestation (mocked GCP metadata)
  - azure_attestation (mocked Azure metadata)
  - k8s_psat_attestation (mocked K8s API)
  - attestation_failure_handling
```

### Test Cases - TPM

```
TestTPMAttestation:
  - tpm_get_endorsement_key
  - tpm_get_attestation_key
  - tpm_quote_generation
  - tpm_quote_verification
  - tpm_seal_unseal_roundtrip
  - tpm_unavailable_fallback
```

---

## Phase 4 Tests: Fleet & Ecosystem

### Integration Tests

| Test Area | What to Test | Environment |
|-----------|--------------|-------------|
| Helm Deployment | Chart installs, upgrades | K3s in Docker |
| Ansible Playbook | Idempotent deployment | Docker containers |
| Fleet Operations | Bulk update, rollback | Multiple satellites |
| Policy Distribution | Group-based artifacts | Ground Control + satellites |

### Test Cases - Fleet Operations

```
TestFleetOperations:
  - bulk_satellite_update
  - bulk_satellite_rollback
  - satellite_decommission
  - group_policy_assignment
  - artifact_distribution_by_group
  - satellite_health_aggregation
```

---

## Test Scenarios by Category

### Connectivity Tests

```
TestConnectivity:
  - normal_operation (stable connection)
  - intermittent_connection (random drops)
  - high_latency (500ms+)
  - packet_loss (5%, 10%, 25%)
  - complete_offline (hours/days)
  - reconnection_after_offline
  - bandwidth_limited (56kbps, 1mbps)
```

### Security Tests

```
TestSecurity:
  - man_in_the_middle_rejected
  - replay_attack_rejected
  - credential_theft_limited_blast_radius
  - tampered_config_detected
  - revoked_certificate_rejected
  - expired_certificate_rejected
  - downgrade_attack_rejected
```

### Resilience Tests

```
TestResilience:
  - power_failure_recovery
  - disk_full_handling
  - memory_exhaustion_handling
  - process_crash_recovery
  - corrupted_config_recovery
  - corrupted_registry_recovery
```

### Idempotency Tests

```
TestIdempotency:
  - registration_idempotent (run twice = same result)
  - state_sync_idempotent
  - config_apply_idempotent
  - credential_rotation_idempotent
  - mirror_config_idempotent
```

---

## TPM Testing Options

### 1. Software TPM (swtpm) - For CI/Dev

```bash
apt install swtpm swtpm-tools
```

- Pros: Free, runs anywhere, good for unit tests
- Cons: Not real attestation, no hardware security
- Use: CI pipelines, local development

### 2. Hardware TPM Module - For Real Testing

- Raspberry Pi: LetsTrust TPM (~$25)
- Intel NUC: Built-in or Infineon SLB9670 (~$20)
- Pros: Real attestation, production-like
- Cons: Requires physical hardware
- Use: Integration tests, E2E tests

### 3. USB TPM - For Portable Testing

- Infineon OPTIGA TPM 2.0 (~$50)
- Pros: Works on any machine with USB
- Cons: More expensive, driver support varies
- Use: Testing on multiple machines

### TPM Test Cases (Hardware Required)

```
TestTPMReal:
  - tpm_present_and_accessible
  - tpm_version_2_0
  - ek_certificate_valid
  - ak_creation_success
  - quote_matches_pcr_state
  - sealed_data_only_unsealable_on_same_device
  - tpm_owner_password_set
```

---

## Test Priority Matrix

| Phase | Test Type | Priority | Blocking Release? |
|-------|-----------|----------|-------------------|
| 1 | Config encryption unit tests | P0 | Yes |
| 1 | Key derivation unit tests | P0 | Yes |
| 1 | TLS setup unit tests | P0 | Yes |
| 1 | Integration tests (Docker) | P1 | Yes |
| 2 | mTLS unit tests | P0 | Yes |
| 2 | Credential rotation unit tests | P0 | Yes |
| 2 | Audit logging unit tests | P1 | Yes |
| 2 | Integration tests (mocked SPIRE) | P1 | Yes |
| 3 | SPIRE integration unit tests | P1 | No (optional) |
| 3 | TPM attestation unit tests | P2 | No (optional) |
| 3 | Hardware tests (real TPM) | P2 | No |
| 4 | Helm chart tests | P1 | No |
| 4 | Fleet operation tests | P2 | No |
| All | E2E on real hardware | P2 | No (recommended) |

---

## Related Documents

- [Mock Interfaces](./mock-interfaces.md)
- [Hardware Shopping List](./hardware-shopping-list.md)
- [Requirements](./requirements.md)
