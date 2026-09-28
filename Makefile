include .bingo/Variables.mk

PROJECT_ID?=gpe-test-1
CLUSTER_NAME?=bwplotka-rw-acc
SCENARIOS?=gmp gmp3 prom-rw-m otel-otlp
MODE?=promql
STAGING?=false
WINDOW?=5m

.PHONY: help
help: ## Display this help and any documented user-facing targets. Other undocumented targets may be present in the Makefile.
help:
	@awk 'BEGIN {FS = ": ##"; printf "Usage:\n  make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_\.\-\/%]+: ##/ { printf "  %-45s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: check-deps
check-deps: $(GOMPLATE) ## Check local dependencies.
	@command -v gcloud >/dev/null 2>&1 || { echo 'Please install gcloud (https://cloud.google.com/sdk/gcloud#download_and_install_the)'; exit 1; }
	@command -v go >/dev/null 2>&1 || { echo 'Please install go (https://go.dev/doc/install)'; exit 1; }
	@command -v kubectl >/dev/null 2>&1 || { echo 'Please install kubectl'; exit 1; }

.PHONY: start
start: check-deps ## Start acceptance test scenario(s) on the current cluster.
	@if [ -n "$(BENCH_NAME)" ]; then \
		scen="$${SCENARIO:-./manifests/scenarios/$(BENCH_NAME)}"; \
		echo "## Starting scenario $(BENCH_NAME) with manifest $$scen"; \
		PROJECT_ID="$(PROJECT_ID)" bash ./scripts/bench-start.sh "$(BENCH_NAME)" "$$scen" "$(CLUSTER_NAME)"; \
	else \
		for s in $(SCENARIOS); do \
			echo "## Starting acceptance scenario $$s"; \
			PROJECT_ID="$(PROJECT_ID)" bash ./scripts/bench-start.sh "$$s" "./manifests/scenarios/$$s" "$(CLUSTER_NAME)"; \
		done; \
	fi

.PHONY: stop
stop: check-deps ## Stop acceptance test scenario(s) on the current cluster.
	@if [ -n "$(BENCH_NAME)" ]; then \
		scen="$${SCENARIO:-./manifests/scenarios/$(BENCH_NAME)}"; \
		echo "## Stopping scenario $(BENCH_NAME) with manifest $$scen"; \
		PROJECT_ID="$(PROJECT_ID)" bash ./scripts/bench-stop.sh "$(BENCH_NAME)" "$$scen" "$(CLUSTER_NAME)"; \
	else \
		for s in $(SCENARIOS); do \
			echo "## Stopping acceptance scenario $$s"; \
			PROJECT_ID="$(PROJECT_ID)" bash ./scripts/bench-stop.sh "$$s" "./manifests/scenarios/$$s" "$(CLUSTER_NAME)"; \
		done; \
	fi

.PHONY: verify
verify: check-deps ## Verify all metric types and over-time data for the scenario(s).
	@if [ -n "$(BENCH_NAME)" ]; then \
		scen="$${SCENARIO:-./manifests/scenarios/$(BENCH_NAME)}"; \
		echo "## Verifying scenario $(BENCH_NAME) with manifest $$scen (mode: $(MODE), staging: $(STAGING), window: $(WINDOW))"; \
		PROJECT_ID="$(PROJECT_ID)" WINDOW="$(WINDOW)" bash ./scripts/bench-verify.sh "$(BENCH_NAME)" "$$scen" "$(CLUSTER_NAME)" "$(MODE)" "$(STAGING)" "$(WINDOW)"; \
	else \
		failed=0; \
		for s in $(SCENARIOS); do \
			echo "## Verifying acceptance scenario $$s (mode: $(MODE), staging: $(STAGING), window: $(WINDOW))"; \
			PROJECT_ID="$(PROJECT_ID)" WINDOW="$(WINDOW)" bash ./scripts/bench-verify.sh "$$s" "./manifests/scenarios/$$s" "$(CLUSTER_NAME)" "$(MODE)" "$(STAGING)" "$(WINDOW)" || failed=1; \
		done; \
		exit $$failed; \
	fi

.PHONY: cluster-setup
cluster-setup: check-deps ## Bootstraps the acceptance test GKE cluster.
	@echo "## Starting/checking cluster $(CLUSTER_NAME) in project $(PROJECT_ID)"
	PROJECT_ID="$(PROJECT_ID)" bash ./scripts/cluster-setup.sh $(CLUSTER_NAME)

.PHONY: cluster-destroy
cluster-destroy: check-deps ## Tear down the acceptance test GKE cluster.
	PROJECT_ID="$(PROJECT_ID)" bash ./scripts/cluster-destroy.sh $(CLUSTER_NAME)

.PHONY: lint
lint: ## Lint resources.
	bash ./scripts/shellcheck.sh

GOMODS := $(shell find . -name "go.mod" | grep -v .bingo | xargs dirname)
.PHONY: test
test:
	@for gomod in $(GOMODS); do \
		(cd $$gomod && go test -v ./...); \
	done

GOFUMPT = gofumpt
$(GOFUMPT):
	@go install mvdan.cc/gofumpt@latest

GO_FILES = $(shell find . -path ./vendor -prune -o -name '*.go' -print)

.PHONY: format
format: $(GOFUMPT) $(GOIMPORTS)
	@echo ">> formating imports"
	@$(GOIMPORTS) -w $(GO_FILES)
	@echo ">> gofumpt-ing the code; golangci-lint requires this"
	@$(GOFUMPT) -extra -w $(GO_FILES)
