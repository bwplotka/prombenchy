#!/usr/bin/env bash
set -efo pipefail
export SHELLOPTS

IFS=$'\t\n'

BENCH_NAME=${1:-}
if [ -z "${BENCH_NAME}" ]; then
    echo "benchmark name is required as the first parameter!"
    exit 1
fi

SCENARIO=${2:-}
CLUSTER_NAME=${3:-}
MODE=${4:-promql}

PROJECT_ID=$(gcloud config get project 2>/dev/null || true)
export PROJECT_ID

echo "## Verifying all metrics are queryable for benchmark ${BENCH_NAME} (mode: ${MODE})"

args=(
  "-bench-name=${BENCH_NAME}"
  "-mode=${MODE}"
)

if [ -n "${SCENARIO}" ]; then
  args+=("-scenario=${SCENARIO}")
fi

if [ -n "${CLUSTER_NAME}" ]; then
  args+=("-cluster-name=${CLUSTER_NAME}")
fi

if [ -n "${PROJECT_ID}" ]; then
  args+=("-project-id=${PROJECT_ID}")
fi

go run ./tools/verify-metrics/main.go "${args[@]}"
