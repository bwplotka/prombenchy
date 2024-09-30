# prombenchy

This repo contains very simplistic, experimental and opinionated [prombench](https://github.com/prometheus/test-infra/tree/master/prombench) alternative
that focuses on benchmarking (and testing) the agents, so collection modes for Prometheus metrics (discovery + scrape + basic processing + remote write/alternative protocols) on GKE.

Feel free to use it as you wish.

### Usage

Check `make help` on what's possible. Then if anything is failing check `scripts/` bash scripts and adjust according to your setup. Those are shell script, will be always flaky for edge cases or races, but it's better than nothing (: 

The general flow looks as follows:

* You setup your GKE cluster once: `make cluster-setup CLUSTER_NAME=my-prombenchy`
* Then to set up any benchmark run you do `make start CLUSTER_NAME=my-prombenchy BENCH_NAME=<name of benchmark, also k8s namespace> SCENARIO=./manifests/scenarios/gmp`. This will setup node-pool and your collector (e.g. as daemon set or separate pod - up to you, as long as you do correct node section!)

You can start as many scenarios as you want on the single cluster (make sure to use unique `BENCH_NAME` though!)

The scenario is a path to the "collector" manifest, so anything that will scrape `./manifests/load/avalanche.exampletarget.yaml`. Feel free to adjust anything in `./manifests/scenarios/` or add your own.

This setup uses separate Prometheus for gathering metrics about core resources and collectors (available locally and in GCM). Make sure your pod has `app=collector` label and relevant port name has `-ins` suffix, sto be scraped by this core Prometheus. There is also a dashboard you can apply to GCM in `./dashboards/`. 

* `make stop CLUSTER_NAME=my-prombenchy BENCH_NAME=<name of benchmark, also k8s namespace> SCENARIO=./manifests/scenarios/gmp` kill the node-pool and experiment.

### TODOs

* [ ] All scenarios are GMP aware, so they send data to GCM. In the future, we plan to also benchmark remote-write or OTLP, but proper test reivers would need to be added. Help welcome!
* [ ] Probably Go code for scripts instead of bash, for reliability.
* [ ] Cleanup svc account permissions on stopped scenarios.
* [ ] Make config-reloader work with otel-collector (annoying to delete pod after config changes).

### Credits

This repo was started by sharing a lot of design and resources from https://github.com/prometheus/test-infra repo, which we maintain in the Prometheus team mostly for [prombench](https://github.com/prometheus/test-infra/tree/master/prombench) functionality. Kudos to prombench project for the hard work so far! Since then, it was completely redesigned and simplified.
