# parsec-client-go (in-tree fork)

This directory is a local fork of [`parallaxsecond/parsec-client-go`](https://github.com/parallaxsecond/parsec-client-go) — the upstream Go client for the CNCF [PARSEC](https://parsec.community/) service. It is consumed by the satellite via a local `replace` directive in the root `go.mod`.

This is a temporary arrangement. Bug fixes made here should be ported upstream; once an upstream tag/commit contains the fixes we need, the satellite should depend on the upstream module directly (`go get github.com/parallaxsecond/parsec-client-go@<sha>`) and this directory should be removed.

## Upstream

- Repository: https://github.com/parallaxsecond/parsec-client-go
- Forked from: (record the upstream commit SHA here when the fork is rebased)

## Local deltas

All local changes are kept minimal and recorded in commit messages with the `fix(parsec-client-go):` or `chore(parsec-client-go):` prefix so they can be replayed upstream. Substantive deltas to date:

- Correctness bugs surfaced by review-bot analysis on harbor-satellite#468 (see commit `fix(parsec-client-go): correctness bugs flagged by review bots`).
- Removed the in-tree `.github/workflows/` (orphaned: GitHub Actions only runs the satellite's top-level workflows; the upstream actions were also several major versions out of date).
- Removed `tmpproto/` — committed-in-error intermediate protoc output.

## Re-merge criteria

When all of the following are true, this directory should be removed and the satellite should depend on upstream directly:

1. Upstream has incorporated (or has equivalent fixes for) every entry under "Local deltas".
2. Upstream has a tagged release or stable commit that builds under the satellite's Go toolchain version.
3. The `replace github.com/parallaxsecond/parsec-client-go => ./parsec-client-go` directive in the root `go.mod` is removed and a `require` line pinned to a real version is added.
