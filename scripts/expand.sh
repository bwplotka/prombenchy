#!/bin/bash

function expand_scenario() {
    source .bingo/variables.env

    BENCH_NAME=$1
    if [ -z "${BENCH_NAME}" ]; then
        echo "benchmark name is required as the first parameter!"
    fi
    SCENARIO=$2
    if [ -z "${SCENARIO}" ]; then
        echo "scenario dir is required as the first parameter!"
    fi

    TEMP_DIR=$(mktemp -d -p "./manifests")
    # Exit if the temp directory wasn't created successfully.
    if [ ! -e "$TEMP_DIR" ]; then
        >&2 echo "Failed to create temp directory"
        exit 1
    fi
    ${GOMPLATE} --input-dir=${SCENARIO} --output-dir=${TEMP_DIR}

    echo "${TEMP_DIR}"
}
