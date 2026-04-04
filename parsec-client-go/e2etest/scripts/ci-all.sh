#!/bin/bash

# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

# Run various tests against parsec daemon in a docker container

SCRIPTDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
TESTDIR=$(realpath "${SCRIPTDIR}"/..)

set -eouf pipefail 

pushd "${TESTDIR}"
docker build -t all-providers "${TESTDIR}"/provider_cfg/all
docker run -v "$(realpath "${TESTDIR}"/..)":/tmp/parsecgo -w /tmp/parsecgo all-providers /tmp/parsecgo/e2etest/scripts/ci.sh all
popd