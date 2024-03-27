#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default

IFS=$'\t\n'

. ./scripts/expand.sh

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

ZONE="us-central1-a"
PROJECT_ID=$(gcloud config get project)

EXPANDED_SCENARIO=$(expand_scenario ${BENCH_NAME} ${SCENARIO})
if [ ! -e "$EXPANDED_SCENARIO" ]; then
    >&2 echo "Failed to expand scenario"
    exit 1
fi
# Make sure the temp directory gets removed on script exit.
trap "exit 1"           HUP INT PIPE QUIT TERM
trap 'rm -r "$EXPANDED_SCENARIO"'  EXIT

echo "## Creating node pool ${BENCH_NAME}-work-pool"
# n2-highmem-8 -- 8 vCPUs 64 GB
gcloud container node-pools create ${BENCH_NAME}-work-pool \
  --project=${PROJECT_ID} \
  --location=${ZONE} \
  --cluster=${CLUSTER_NAME} \
  --max-pods-per-node=200 \
  --machine-type=n2-highmem-8 \
  --labels="role=${BENCH_NAME}-work" \
  --num-nodes=1

kubectl apply -f "${EXPANDED_SCENARIO}"
