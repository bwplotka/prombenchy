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
if gcloud container node-pools list --cluster="${CLUSTER_NAME}" --zone=${ZONE} 2>&1 | grep -q "^${BENCH_NAME}-work-pool "
then
  echo "WARN: node pool ${BENCH_NAME}-work-pool already exists, skipping creation"
else
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
fi

echo "## Applying scenario resources"
# All scenarios has the same load and requires GMP operator. Make it more flexible
# if needed later on.
#kubectlExpandApply "./manifests/gmp-operator" #?
kubectlExpandApply "./manifests/load/avalanche.yaml"
kubectlExpandApply "${SCENARIO}"
