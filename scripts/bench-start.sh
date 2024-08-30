#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default

IFS=$'\t\n'

. ./scripts/util.sh

ZONE="us-central1-a"
export PROJECT_ID=$(gcloud config get project)

BENCH_NAME=$1
if [ -z "${BENCH_NAME}" ]; then
    echo "benchmark name is required as the first parameter!"
    exit 1
fi
SCENARIO=$2
if [ -z "${SCENARIO}" ]; then
    echo "scenario dir is required as the second parameter!"
    ecit 1
fi

CLUSTER_NAME=$3
if [ -z "${CLUSTER_NAME}" ]; then
    echo "cluster name is required as the third parameter!"
    exit 1
fi

echo "## Assuming ${CLUSTER_NAME}"
gcloud container clusters get-credentials ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_ID}

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
    --node-labels="role=${BENCH_NAME}-work" \
    --num-nodes=1
fi

echo "## Applying scenario resources"

# TODO(bwplotka): All scenarios has the same load and requires GMP operator. Make it more flexible
# if needed later on.
# kubectlExpandApply "./manifests/gmp-operator"
kubectlExpandApply "./manifests/load/avalanche.yaml"
kubectlExpandApply "${SCENARIO}"
