# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

# Explicit tag — never rely on whatever happens to be tagged `latest`.
ARG BASE_TAG=latest
FROM parsec-ci-service-test-all:${BASE_TAG}

# Build-only stage runs as root for toolchain installs; drop back to a
# non-root user at the end (Codacy DL3002).
USER root

WORKDIR /tmp

# Install go 1.18 (with checksum verification — never extract an unverified tarball)
ARG GO_VERSION=1.18.10
ARG GO_SHA256=5e05400e4c79ef5394424c0eff5b9141cb782da25f64f79d54c98af0a37f8d49
RUN curl -fsSL -o /tmp/go.tgz "https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
    && echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c - \
    && tar -xz -f /tmp/go.tgz -C /usr/local \
    && rm /tmp/go.tgz
ENV PATH="/usr/local/go/bin:${PATH}"

# Pin parsec to a specific commit for reproducible builds.
# Default is a tag for readability — pass `--build-arg PARSEC_COMMIT=<sha>` in
# CI to harden against tag movement.
ARG PARSEC_COMMIT=1.4.0
WORKDIR /tmp
RUN git clone https://github.com/parallaxsecond/parsec \
    && cd parsec \
    && git checkout "${PARSEC_COMMIT}" \
    && git submodule update --init \
    && RUST_LOG=info RUST_BACKTRACE=1 cargo build --features=all-providers,all-authenticators

WORKDIR /tmp

# Drop privileges. The CI base image provides the `parsec` user.
USER parsec