# TLS

## Ground Control Server

Set both `TLS_CERT_FILE` and `TLS_KEY_FILE` to serve HTTPS. The minimum version is TLS 1.2. Ground Control checks the files every 30 seconds and loads a renewed certificate without a restart, so cert-manager or an ACME client can rotate them in place.

`TLS_CA_FILE` turns on client certificate verification: every client, operators and satellites included, must then present a certificate signed by that CA.

With `SPIFFE_SERVER_ENABLED=true` Ground Control serves mTLS with its own SVID instead, and the `TLS_*` files are not used.

Behind a TLS-terminating reverse proxy, run Ground Control on plain HTTP inside the trusted network. Then:

- satellites still reach Ground Control over HTTPS through the proxy;
- the OIDC browser flow works, because the client sees HTTPS;
- set `AUDIT_TRUST_FORWARDED_HEADERS=true` and `RATE_LIMIT_SPLIT=true`, so audit records, the failure limit and the per-IP limits see the client address instead of the proxy's.

## Satellite Client

| Setting | Effect |
|---|---|
| `app_config.tls.ca_file` | CA bundle for Harbor, Ground Control and the BYO registry. |
| `app_config.tls.cert_file`, `key_file` | Client certificate, for a Ground Control with `TLS_CA_FILE`. |
| `app_config.tls.skip_verify` | Skip certificate verification. Development only. |
| `--use-unsecure` / `USE_UNSECURE` | Plain HTTP and no verification. Required for an `http://` Ground Control URL. Development only. |
| `--spiffe-enabled` | mTLS with the satellite's SVID; `--spiffe-expected-server-id` pins Ground Control's SPIFFE ID. |

## Database

`DB_SSLMODE` defaults to `require`, which encrypts but does not verify the server. Use `verify-full` with `DB_SSLROOTCERT` for a database on another host. See [Database Operations](database.md).

## Webhook Receivers

Webhook URLs must be `https`, except on a loopback host. Ground Control verifies receiver certificates against the system trust store.
