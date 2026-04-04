# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

FROM   parsec-ci-service-test-all

# Install Rust toolchain for root
USER root

WORKDIR /tmp

# Install go 1.18

RUN curl -s -N -L https://golang.org/dl/go1.18.linux-amd64.tar.gz | tar  xz -C /usr/local
ENV PATH="/usr/local/go/bin:${PATH}"

RUN git clone https://github.com/parallaxsecond/parsec

WORKDIR parsec
# Initialising any submodules. Currently used for building the Trusted Service provider
RUN git submodule update --init
RUN RUST_LOG=info RUST_BACKTRACE=1 cargo build --features=all-providers,all-authenticators

WORKDIR /tmp