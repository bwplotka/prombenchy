#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default

IFS=$'\t\n'

CLUSTER_NAME=$1
if [ -z "${CLUSTER_NAME}" ]; then
    echo "cluster name is required as the first parameter!"
fi

echo "not implemented yet"
exit 1
