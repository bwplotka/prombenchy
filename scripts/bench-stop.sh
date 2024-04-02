#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default

IFS=$'\t\n'

. ./scripts/util.sh

ZONE="us-central1-a"
PROJECT_ID=$(gcloud config get project)

BENCH_NAME=$1
if [ -z "${BENCH_NAME}" ]; then
    echo "benchmark name is required as the first parameter!"
fi
SCENARIO=$2
if [ -z "${SCENARIO}" ]; then
    echo "scenario dir is required as the first parameter!"
fi

CLUSTER_NAME=$(kubectl config current-context | rev | cut -d_ -f1 | rev)
if [ -z "${CLUSTER_NAME}" ]; then
    >&2 echo "Failed to fetch cluster name"
    exit 1
fi

echo "## Assuming ${CLUSTER_NAME}"

# All scenarios has the same load and requires GMP operator. Make it more flexible
# if needed later on.
kubectlExpandDelete "./manifests/gmp-operator"
kubectlExpandDelete "./manifests/load/avalanche.yaml"
kubectlExpandDelete "${SCENARIO}"

# n2-highmem-8 -- 8 vCPUs 64 GB
gcloud container node-pools delete ${BENCH_NAME}-work-pool
