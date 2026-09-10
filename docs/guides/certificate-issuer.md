# Certificate Issuer (TLS/SPIFFE)

This guide explains the certificate issuer chain in Harbor Satellite SPIFFE deployments. For the full walkthrough with diagrams and troubleshooting, see the [website documentation](https://satellite.container-registry.com/docs/certificate-issuer/).

## Summary

Harbor Satellite uses up to four certificate layers when SPIFFE is enabled:

1. **SPIRE upstream authority CA** (`CN=SPIRE CA`) — Trust root. Signs SPIRE's intermediate CA, which issues all SVIDs. Configured via `UpstreamAuthority "disk"` in SPIRE server config.
2. **X.509 PoP CA** (`CN=X509 PoP CA`) — Signs agent attestation certificates. Only used with x509pop attestation. Trusted by SPIRE via `ca_bundle_path`.
3. **Agent attestation certificates** — Leaf certs signed by the PoP CA. CN must match `satellite_name` (the x509pop example uses `agent-satellite` in both `generate-certs.sh` and `setup.sh`). Used once at node attestation.
4. **Workload SVIDs** — Short-lived certs issued by SPIRE server CA. Used for mTLS between Ground Control and Satellite. Auto-rotated.

## Quick Inspection

```bash
# Issuer and subject of an agent cert (x509pop example output)
openssl x509 -in certs/agent-satellite.crt -noout -issuer -subject

# Verify chain to PoP CA
openssl verify -CAfile certs/x509pop-ca.crt certs/agent-satellite.crt

# List attested agents
docker exec spire-server /opt/spire/bin/spire-server agent list \
    -socketPath /tmp/spire-server/private/api.sock
```

## Certificate Generation Scripts

| Attestation | Script |
|-------------|--------|
| x509pop | `examples/deploy/spiffe/x509pop/external/gc/generate-certs.sh` |
| join-token | `examples/deploy/spiffe/join-token/external/gc/generate-certs.sh` |
| sshpop | `examples/deploy/spiffe/sshpop/external/gc/generate-certs.sh` |

## Related

- [ADR 0005: SPIFFE Identity and Security](../decisions/0005-spiffe-identity-and-security.md)
- [K3s Reference Architecture](k3s-reference-architecture.md)
- [SPIFFE examples](https://github.com/container-registry/harbor-satellite/tree/main/examples/deploy/spiffe)
