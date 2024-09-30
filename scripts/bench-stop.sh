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

CLUSTER_NAME=$3
if [ -z "${CLUSTER_NAME}" ]; then
    echo "cluster name is required as the third parameter!"
    exit 1
fi

echo "## Assuming ${CLUSTER_NAME}"
gcloud container clusters get-credentials ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_ID}

export BENCH_NAME
export PROJECT_ID
export ZONE
export CLUSTER_NAME

kubectlExpandDelete "./manifests/load/avalanche.yaml"
kubectlExpandDelete "${SCENARIO}"

# n2-highmem-8 -- 8 vCPUs 64 GB
gcloud container node-pools delete --async --cluster ${CLUSTER_NAME} --zone ${ZONE} ${BENCH_NAME}-work-pool




