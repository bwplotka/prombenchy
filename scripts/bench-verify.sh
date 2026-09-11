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
STAGING=${5:-${STAGING:-false}}

PROJECT_ID=$(gcloud config get project 2>/dev/null || true)
export PROJECT_ID

echo "## Verifying all metrics are queryable for benchmark ${BENCH_NAME} (mode: ${MODE}, staging: ${STAGING})"

args=(
  "-bench-name=${BENCH_NAME}"
  "-mode=${MODE}"
)

if [ "${STAGING}" = "true" ] || [ "${STAGING}" = "1" ]; then
  args+=("-staging=true")
fi

if [ -n "${SCENARIO}" ]; then
  args+=("-scenario=${SCENARIO}")
fi

if [ -n "${CLUSTER_NAME}" ]; then
  args+=("-cluster-name=${CLUSTER_NAME}")
fi

if [ -n "${PROJECT_ID}" ]; then
  args+=("-project-id=${PROJECT_ID}")
fi

if [ -n "${TOKEN:-}" ]; then
  args+=("-token=${TOKEN}")
fi

go run ./tools/verify-metrics/main.go "${args[@]}"
