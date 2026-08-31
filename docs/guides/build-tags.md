# Build tags

Harbor Satellite builds with no build tags by default. Released binaries are
produced that way: the `Dockerfile` defaults `GO_TAGS` to empty and
`.goreleaser.yaml` sets no tags. The tags below change what the binary is
capable of, so read what each one removes before using it.

## `nospiffe`

Drops the SPIFFE dependencies, along with the cryptographic provider.

**This build cannot encrypt anything.** `internal/crypto` compiles
`UnavailableProvider` instead of the AES-GCM provider, and every operation —
`Encrypt`, `Decrypt`, `Sign`, `Verify`, `DeriveKey`, `GenerateKeyPair`,
`RandomBytes` — returns `crypto.ErrCryptoUnavailable`. `Hash` returns nil,
because the `Provider` interface gives it no error to return.

The practical consequences:

| Feature | `nospiffe` build |
| --- | --- |
| SPIFFE identity and mTLS | unavailable, token-only operation |
| Config encryption at rest (`encrypt_config: true`) | **unavailable — writes are refused** |
| Reading an existing encrypted config | **unavailable — fails to decrypt** |
| Signature verification | unavailable, never reports success |

Setting `encrypt_config: true` in a `nospiffe` build makes every config write
fail with `crypto.ErrCryptoUnavailable`, rather than writing the credentials in
the clear. If you need this build, either leave `encrypt_config` off and accept
a plaintext config on disk, or protect the config by other means (file
permissions, an encrypted volume).

This is deliberate. The provider used to fail open: `Encrypt` returned the
plaintext unchanged, `Verify` reported every signature as valid without looking
at it, and `RandomBytes` returned zeros, so every salt was identical. A config
written by such a build still carried the version header that marks a file as
encrypted, and `secure.IsEncrypted` still recognised it, so Harbor robot
credentials sat on the edge device in readable form with nothing to show that
encryption had not happened. Failing closed is louder but honest.

`internal/satellite/identity` behaves the same way under this tag (and on
non-Linux hosts): device fingerprinting returns `ErrComponentUnavailable`.

Build and test it with:

```sh
go build -tags nospiffe ./...
go test -tags nospiffe ./...
```

Both run in CI (the `nospiffe-build` job in `.github/workflows/test.yaml`).
Tests that exercise the real cryptography are tagged `!nospiffe`; the
fail-closed behaviour has its own tests under `nospiffe`.

## `parsec`

Opts into the PARSEC code path for key management. Independent of `nospiffe` —
see [ADR 0007](../decisions/0007-security-plugins-parsec.md) for the supported
combinations.

```sh
go build -tags parsec ./...
```
