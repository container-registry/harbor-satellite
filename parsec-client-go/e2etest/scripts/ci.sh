#!/usr/bin/env bash

# Copyright 2021 Contributors to the Parsec project.
# SPDX-License-Identifier: Apache-2.0

# Run parsec daemon and then run test suites as defined by parameters (either all providers or a single provider)
# This script is run by the docker based ci build environment and is not intended to be run separately
# To run this for all provider tests, run ./ci-all.sh in this folder (you will need docker installed)


SCRIPTDIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"
TESTDIR=$(realpath "${SCRIPTDIR}"/..)
set -ex

# The clean up procedure is called when the script finished or is interrupted
cleanup () {
    echo "Shutdown Parsec and clean up"
    # Stop Parsec if running
    stop_service
    # Stop tpm_server if running
    if [ -n "$TPM_SRV_PID" ]; then kill "$TPM_SRV_PID" || true; fi
    if [ -n "$TPM_MC_SRV_PID" ]; then kill "$TPM_MC_SRV_PID" || true; fi
    # Remove the slot_number line added earlier
    find e2etest -name "*toml" -not -name "Cargo.toml" -exec sed -i 's/^slot_number =.*/# slot_number/' {} \;
    find e2etest -name "*toml" -not -name "Cargo.toml" -exec sed -i 's/^serial_number =.*/# serial_number/' {} \;
    # Remove fake mapping and temp files
    rm -rf "mappings" "kim-mappings"
    rm -f "NVChip"
    rm -f "e2etest/provider_cfg/tmp_config.toml"
    rm -f "parsec.sock"

    if [ -z "$NO_GO_CLEAN" ]; then go clean; fi
}

usage () {
    printf "
Continuous Integration test script

This script will execute various tests targeting a platform with a
single provider or all providers included.
It is meant to be executed inside one of the container
which Dockerfiles are in tests/per_provider/provider_cfg/*/
or tests/all_providers/

Usage: ./ci.sh [--no-go-clean] [--no-stress-test] PROVIDER_NAME
where PROVIDER_NAME can be one of:
    - mbed-crypto
    - pkcs11
    - tpm
    - all
"
}

error_msg () {
    echo "Error: $1"
    usage
    exit 1
}

wait_for_service() {
    while [ -z "$(pgrep parsec)" ]; do
        sleep 1
    done

    sleep 5

    # Check that Parsec successfully started and is running
    pgrep parsec >/dev/null
}

stop_service() {
    # Randomly signals with SIGINT or SIGTERM to test that both can be used to
    # gracefully shutdowm Parsec.
    # shellcheck disable=SC2004
    if ! (($RANDOM % 2)); then
        pkill -SIGINT parsec || true
    else
        pkill -SIGTERM parsec || true
    fi

    while [ -n "$(pgrep parsec)" ]; do
        sleep 1
    done
}

reset_tpm()
{
    # In order to reset the TPM, we need to restart the TPM server and send a Startup(CLEAR)
    pkill tpm_server
    sleep 1

    tpm_server &
    TPM_SRV_PID=$!
    sleep 5

    tpm2_startup -c -T mssim
}

reload_service() {
    echo "Trigger a configuration reload to load the new mappings or config file"
    pkill -SIGHUP parsec
    sleep 5
}

# Parse arguments
PROVIDER_NAME=
CONFIG_PATH=${TESTDIR}/provider_cfg/tmp_config.toml
while [ "$#" -gt 0 ]; do
    case "$1" in
        mbed-crypto | pkcs11 | tpm | all )
            if [ -n "$PROVIDER_NAME" ]; then
                error_msg "Only one provider name must be given"
            fi
            PROVIDER_NAME=$1
            cp "${TESTDIR}"/provider_cfg/"$1"/config.toml "$CONFIG_PATH"
        ;;
        *)
            error_msg "Unknown argument: $1"
        ;;
    esac
    shift
done

# Check if the PROVIDER_NAME was given.
if [ -z "$PROVIDER_NAME" ]; then
    error_msg "a provider name needs to be given as input argument to that script."
fi

trap cleanup EXIT

if [ "$PROVIDER_NAME" = "tpm" ] || [ "$PROVIDER_NAME" = "all" ]; then
    echo  Start and configure TPM server
	# Copy the NVChip for previously stored state. This is needed for the key mappings test.
    cp /tmp/ondisk/NVChip .
    # Start and configure TPM server
    tpm_server &
    TPM_SRV_PID=$!
    sleep 5
    # The -c flag is not used because some keys were created in the TPM via the generate-keys.sh
    # script. Ownership has already been taken with "tpm_pass".
    tpm2_startup -T mssim

    # Start and configure TPM server for MakeCredential
    TPM_MC_PORT=4321
    mkdir -p /tmp/mc_tpm
    pushd /tmp/mc_tpm
    tpm_server -port $TPM_MC_PORT &
    TPM_MC_SRV_PID=$!
    sleep 5
    tpm2_startup -c -T mssim:port=$TPM_MC_PORT
    popd
fi

if [ "$PROVIDER_NAME" = "pkcs11" ] || [ "$PROVIDER_NAME" = "all" ] || [ "$PROVIDER_NAME" = "coverage" ]; then
    pushd e2etest
    # This command suppose that the slot created by the container will be the first one that appears
    # when printing all the available slots.
    SLOT_NUMBER=$(softhsm2-util --show-slots | head -n2 | tail -n1 | cut -d " " -f 2)
    # shellcheck disable=SC2196
    SERIAL_NUMBER=$(softhsm2-util --show-slots | grep "Serial number:*" | head -n1 | egrep -ow "[0-9a-zA-Z]+" | tail -n1)
    # Find all TOML files in the directory (except Cargo.toml) and replace the commented slot number with the valid one
    find . -name "*toml" -not -name "Cargo.toml" -exec sed -i "s/^# slot_number.*$/slot_number = $SLOT_NUMBER/" {} \;
    find . -name "*toml" -not -name "Cargo.toml" -exec sed -i "s/^# serial_number.*$/serial_number = \"$SERIAL_NUMBER\"/" {} \;
    popd
fi

mkdir -p /run/parsec

echo "Start Parsec for end-to-end tests"
RUST_LOG=info RUST_BACKTRACE=1 /tmp/parsec/target/debug/parsec --config "$CONFIG_PATH" &

# Sleep time needed to make sure Parsec is ready before launching the tests.
sleep 5

# Check that Parsec successfully started and is running
pgrep -f /tmp/parsec/target/debug/parsec >/dev/null
export PARSEC_SERVICE_ENDPOINT=unix:/run/parsec/parsec.sock
pushd "${TESTDIR}" || exit
go test -v --tags=end2endtest ./... 
popd || exit