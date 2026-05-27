# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

FROM   parsec-ci-service-test-all

# Install Rust toolchain for root
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
ARG PARSEC_COMMIT=1.4.0
RUN git clone https://github.com/parallaxsecond/parsec && cd parsec && git checkout "${PARSEC_COMMIT}"

WORKDIR parsec
# Initialising any submodules. Currently used for building the Trusted Service provider
RUN git submodule update --init
RUN RUST_LOG=info RUST_BACKTRACE=1 cargo build --features=all-providers,all-authenticators

WORKDIR /tmp