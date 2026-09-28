#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default

IFS=$'\t\n'

. ./scripts/util.sh

ZONE="us-central1-a"
PROJECT_ID=${PROJECT_ID:-$(gcloud config get project)}
export PROJECT_ID

BENCH_NAME=$1
if [ -z "${BENCH_NAME}" ]; then
    echo "scenario/benchmark name is required as the first parameter!"
    exit 1
fi
SCENARIO=$2
if [ -z "${SCENARIO}" ]; then
    echo "scenario dir is required as the second parameter!"
    exit 1
fi

CLUSTER_NAME=$3
if [ -z "${CLUSTER_NAME}" ]; then
    echo "cluster name is required as the third parameter!"
    exit 1
fi

echo "## Assuming ${CLUSTER_NAME} (project: ${PROJECT_ID})"
gcloud container clusters get-credentials "${CLUSTER_NAME}" --zone "${ZONE}" --project "${PROJECT_ID}"

# Performing steps for workload identity https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity#authenticating_to and https://cloud.google.com/stackdriver/docs/managed-prometheus/setup-unmanaged#gmp-wli-svcacct
gcloud iam service-accounts add-iam-policy-binding \
  --project="${PROJECT_ID}" \
  --role roles/iam.workloadIdentityUser \
  --member "serviceAccount:${PROJECT_ID}.svc.id.goog[${BENCH_NAME}/collector]" \
  "gmp-prombench@${PROJECT_ID}.iam.gserviceaccount.com" \
  --quiet

echo "## Applying scenario resources for ${BENCH_NAME} from ${SCENARIO}"

export BENCH_NAME
export PROJECT_ID
export ZONE
export CLUSTER_NAME

kubectlExpandApply "${SCENARIO}"
