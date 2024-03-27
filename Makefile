include .bingo/Variables.mk

CLUSTER_NAME?=bwplotka-prombenchy
SCENARIO="./manifests/scenarios/gmp-high-load-node"

.PHONY: help
help: ## Display this help and any documented user-facing targets. Other undocumented targets may be present in the Makefile.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/%]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: check-deps
check-deps: ## Check local dependencies.
	@command -v gcloud >/dev/null 2>&1 || { echo 'Please install gcloud (https://cloud.google.com/sdk/gcloud#download_and_install_the)'; exit 1; }
	@command -v go >/dev/null 2>&1 || { echo 'Please install go (https://go.dev/doc/install)'; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo 'Please install kubectl'; exit 1; }

.PHONY: start
start: check-deps ## Start benchmark on the current cluster.
	@test -n "$(BENCH_NAME)" || (echo "BENCH_NAME variable is not set, what name for this benchmark you want to use?" ; exit 1)
	@# TODO(bwplotka): Check against cluster mismatches.
	@# TODO(bwplotka): Check if this benchmark is already running.
	@echo "## Starting benchmark $(BENCH_NAME) with scenario $(SCENARIO)"
	bash ./scripts/bench-start.sh $(BENCH_NAME) $(SCENARIO)

.PHONY: stop
stop: check-deps ## Start benchmark on the current cluster.
	@test -n "$(BENCH_NAME)" || (echo "BENCH_NAME variable is not set, what name for this benchmark you want to use?" ; exit 1)
	@# TODO(bwplotka): Check against cluster mismatches.
	@# TODO(bwplotka): Make sure scenario is not needed here..
	@echo "## Stopping benchmark $(BENCH_NAME) with scenario $(SCENARIO)"
	bash ./scripts/bench-stop.sh $(BENCH_NAME) $(SCENARIO)

.PHONY: cluster-setup
cluster-setup: check-deps ## Bootstraps the benchmarking GKE cluster.
	@echo "## Starting/checking cluster"
	bash ./scripts/cluster-setup.sh $(CLUSTER_NAME)


.PHONY: cluster-destoy
cluster-destroy: check-deps ## Tear down the benchmarking GKE cluster.
	bash ./scripts/cluster-destroy.sh $(CLUSTER_NAME)

.PHONY: lint
lint: ## Lint resources.
	bash ./scripts/shellcheck.sh
