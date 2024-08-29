#!/bin/bash
set -efo pipefail
export SHELLOPTS	# propagate set to children by default
IFS=$'\t\n'
source .bingo/variables.env

CLUSTER_NAME=$1
if [ -z "${CLUSTER_NAME}" ]; then
    echo "cluster name is required as the first parameter!"
fi

ZONE="us-central1-a"
PROJECT_ID=$(gcloud config get project)

# Do nothing if cluster already exists.
if gcloud container clusters list --filter="name: ${CLUSTER_NAME}" 2>&1 | grep -q "^${CLUSTER_NAME} "
then
  echo "WARN: Cluster ${CLUSTER_NAME} already exists, skipping creation"
  gcloud container clusters get-credentials ${CLUSTER_NAME} --zone ${ZONE} --project ${PROJECT_ID}
else
  # Start a new cluster.
  # https://cloud.google.com/sdk/gcloud/reference/container/clusters/create
  # n2-standard-4 -- 4 vCPUs 16 GB
  gcloud container clusters create ${CLUSTER_NAME} \
    --project=${PROJECT_ID} \
    --location=${ZONE} \
    --workload-pool=${PROJECT_ID}.svc.id.goog \
    --release-channel=rapid \
    --num-nodes=1 \
    --node-labels="role=core" \
    --machine-type="n2-standard-4" \
    --disk-size=300 \
    --no-enable-managed-prometheus # We will have our own monitoring here.
fi

CLUSTER_API_URL=$(kubectl config view --minify -o jsonpath="{.clusters[?(@.name == \"kind-${CLUSTER_NAME}\")].cluster.server}")
echo "## Cluster is now running, kubectl should point to the new cluster at ${CLUSTER_API_URL}"
kubectl cluster-info

TEMP_DIR=$(mktemp -d -p "./manifests")
# Exit if the temp directory wasn't created successfully.
if [ ! -e "$TEMP_DIR" ]; then
    >&2 echo "Failed to create temp directory"
    exit 1
fi
# Make sure the temp directory gets removed on script exit.
trap "exit 1"           HUP INT PIPE QUIT TERM
trap 'rm -r "$TEMP_DIR"'  EXIT

# Make sure the gmp-prombench Service Account exists
SA="gmp-prombench"
if ! gcloud iam service-accounts list --project=${PROJECT_ID} | grep ${SA}
then
  gcloud iam service-accounts create "${SA}" --project=${PROJECT_ID} \
  --description="A service account just to used for the core GMP manifests" \
  --display-name="GMP Prombench Service Account" \
  --quiet
fi

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${SA}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/iam.serviceAccountUser" \
  --quiet

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${SA}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/monitoring.metricWriter" \
  --quiet

gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
  --member="serviceAccount:${SA}@${PROJECT_ID}.iam.gserviceaccount.com" \
  --role="roles/monitoring.metricWriter" \
  --quiet

gcloud iam service-accounts add-iam-policy-binding ${SA}@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:${PROJECT_ID}.svc.id.goog[core/prometheus]" \
    --project ${PROJECT_ID}

gcloud iam service-accounts add-iam-policy-binding ${SA}@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:${PROJECT_ID}.svc.id.goog[gmp-system/operator]" \
    --project ${PROJECT_ID}

gcloud iam service-accounts add-iam-policy-binding ${SA}@${PROJECT_ID}.iam.gserviceaccount.com \
    --role roles/iam.workloadIdentityUser \
    --member "serviceAccount:${PROJECT_ID}.svc.id.goog[gmp-system/collector]" \
    --project ${PROJECT_ID}

echo "## Installing core resources"
PROJECT_ID=${PROJECT_ID} ${GOMPLATE} --input-dir=./manifests/core --output-dir="${TEMP_DIR}"
kubectl apply -f "${TEMP_DIR}"
