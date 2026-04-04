#!/bin/bash

# Copyright 2022 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

# Builds the main parsec ci test docker image
# and tags it for local use

set -e

PARSEC_CI_DOCKER_IMAGE_TAG=parsec-ci-service-test-all

cleanup() {
    echo "Cleaning up"
    popd
    if [[ -n "${PARSEC_CI_FOLDER}" ]]
    then 
        if [ -d "${PARSEC_CI_FOLDER}" ]
        then 
            rm -Rf "${PARSEC_CI_FOLDER}"
        fi
    fi

}

trap cleanup EXIT

PARSEC_CI_FOLDER=$(mktemp -d)

# Get the docker file and associated scripts from main branch on parsec server repo
PARSEC_DOCKER_FILES_BASE=https://raw.githubusercontent.com/parallaxsecond/parsec/main/e2e_tests/docker_image/

pushd "${PARSEC_CI_FOLDER}"

wget -q ${PARSEC_DOCKER_FILES_BASE}/_exec_wrapper
wget -q ${PARSEC_DOCKER_FILES_BASE}/cross-compile-tss.sh
wget -q ${PARSEC_DOCKER_FILES_BASE}/generate-keys.sh
wget -q ${PARSEC_DOCKER_FILES_BASE}/import-old-e2e-tests.sh
wget -q ${PARSEC_DOCKER_FILES_BASE}/parsec-service-test-all.Dockerfile
chmod +x _exec_wrapper ./*.sh

# and build the image
docker build -t "${PARSEC_CI_DOCKER_IMAGE_TAG}" -f parsec-service-test-all.Dockerfile .

