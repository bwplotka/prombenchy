#!/bin/bash

source .bingo/variables.env

function kubectlExpandApply() {
    DIR=$1
    if [ -z "${DIR}" ]; then
        echo "dir is required as the first parameter!"
    fi

    find "${DIR}" -type f -name "*.yaml" -print0 | sort -z | xargs -0 -n1 cat | ${GOMPLATE} | kubectl apply -f -
}

function kubectlExpandDelete() {
    DIR=$1
    if [ -z "${DIR}" ]; then
        echo "dir is required as the first parameter!"
    fi

    find "${DIR}" -type f -name "*.yaml" -print0 | sort -z | xargs -0 -n1 cat | ${GOMPLATE} | kubectl delete -f -
}

