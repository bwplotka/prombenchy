# Build from the open-telemetry/otel-arrow repository root (branch bwplotka/prometheus-receiver-gcp-auth):
#   docker build -f manifests/scenarios/otap-rust/Dockerfile \
#     -t gcr.io/gpe-test-1/otap-df-engine:prometheus-receiver-gcp-auth-v0.1.0 /path/to/otel-arrow
FROM rust:1.99-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*

WORKDIR /workspace
COPY . .

WORKDIR /workspace/rust/otap-dataflow
RUN cargo build --locked --release --features gcp-auth --bin df_engine

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /workspace/rust/otap-dataflow/target/release/df_engine /df_engine
USER 1000:1000
ENTRYPOINT ["/df_engine"]
