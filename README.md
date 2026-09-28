# prombenchy (Acceptance Test Mode)

This branch configures `prombenchy` as an **acceptance test suite** (rather than a high-cardinality load benchmark) for validating Prometheus metric collection and ingestion across multiple GKE collector scenarios against a single shared [Avalanche](https://github.com/prometheus-community/avalanche) target.

## Architecture

* **Single Shared Target (`core/avalanche`)**: A single 1-replica `avalanche` pod runs in the `core` namespace ([manifests/core/3_avalanche.yaml](./manifests/core/3_avalanche.yaml)) and exposes **1 metric family per type** (`GAUGE`, `COUNTER`, `HISTOGRAM (classic)`, `HISTOGRAM (native)`, `SUMMARY`) with **1 series per metric** (`--series-count=1`, `--series-interval=0`, `--value-interval=15`).
* **Shared `core` Node Pool**: All scenario collectors run alongside `core` resources on the `core` node pool and scrape the same `core/avalanche` pod, relabeling `namespace` to their own scenario name so metrics do not collide in Google Cloud Monitoring.
* **Default Acceptance Scenarios**:
  * `gmp`: Prometheus v2 running with Google Managed Prometheus engine exporting to Google Cloud Monitoring.
  * `gmp3`: Prometheus v3 running with Google Managed Prometheus engine exporting to Google Cloud Monitoring.
  * `prom-rw-m`: Upstream OSS Prometheus using Prometheus Remote Write v2 (`io.prometheus.write.v2.Request`) with `--enable-feature=metadata-wal-records` and `google_iam` auth.
  * `otel-otlp`: OpenTelemetry Collector (`otelcol-google`) scraping targets and exporting directly via OTLP gRPC to Google Cloud Telemetry API (`telemetry.googleapis.com:443`).
  * *(Additional scenarios in `./manifests/scenarios/`: `prom-rw-m-unkn`, `prom-rw`, `prom-rw-nost`, `gmp-otel`)*

## Usage

Check `make help` for available targets. By default, `PROJECT_ID=gpe-test-1`, `CLUSTER_NAME=bwplotka-rw-acc`, `SCENARIOS="gmp gmp3 prom-rw-m otel-otlp"`, and `WINDOW=5m`.

1. **Setup the GKE cluster and `core` resources (including `core/avalanche`)**:
   ```bash
   make cluster-setup PROJECT_ID=gpe-test-1 CLUSTER_NAME=bwplotka-rw-acc
   ```

2. **Start all default acceptance scenarios (`gmp`, `gmp3`, `prom-rw-m`, `otel-otlp`)**:
   ```bash
   make start
   # Or start a single scenario:
   make start BENCH_NAME=gmp
   ```

3. **Verify over-time metric ingestion across the time window (`WINDOW=5m`)**:
   ```bash
   make verify WINDOW=5m
   # Or verify a single scenario:
   make verify BENCH_NAME=prom-rw-m WINDOW=5m
   ```

4. **Stop scenarios**:
   ```bash
   make stop
   # Or stop a single scenario:
   make stop BENCH_NAME=gmp
   ```
