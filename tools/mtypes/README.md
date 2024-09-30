# mtypes

Go CLI gathering statistics around the distribution of types, average number of buckets (and more) across your Prometheus metrics/series.

## Usage

The main usage allows to take resource (from stdin, file or HTTP /metrics endpoint) and calculate type statistics e.g.:

```bash
$ mtypes -resource=http://localhost:9090/metrics
$ mtypes -resource=./metrics.prometheus.txt
$ cat ./metrics.prometheus.txt | mtypes
```

```bash 
Metric Type    Metric Families    Series    Series %     Series % (complex type adjusted)    Average Buckets/Objectives
GAUGE          77                 94        30.618893    15.112540                           -
COUNTER        104                167       54.397394    26.848875                           -
HISTOGRAM      11                 19        6.188925     39.710611                           11.000000
SUMMARY        15                 27        8.794788     18.327974                           2.222222
```

> NOTE: "Adjusted" series, means actual number of individual series stored in Prometheus. Classic histograms and summaries are stored as a set of counters. This is relevant as the cost of indexing new series is higher than storing complex values (this is why we slowly move to native histograms).

Additionally, you can pass `--avalanche-flags-for-adjusted-series=10000` to print Avalanche v0.6.0+ flags to configure, for avalanche to generate metric target with the given amount of adjusted series, while maintaining a similar distribution e.g.

```bash
cat ../../manifests/load/exampleprometheustarget.txt | go run main.go --avalanche-flags-for-adjusted-series=10000
Metric Type    Metric Families    Series (adjusted)    Series (adjusted) %        Average Buckets/Objectives
GAUGE          77                 94 (94)              30.921053 (15.719064)      -
COUNTER        104                166 (166)            54.605263 (27.759197)      -
HISTOGRAM      11                 17 (224)             5.592105 (37.458194)       11.176471
SUMMARY        15                 27 (114)             8.881579 (19.063545)       2.222222
---            ---                ---                  ---                        ---
*              207                304 (598)            100.000000 (100.000000)    -

Avalanche flags for the similar distribution to get to the adjusted series goal of: 10000
--gauge-metric-count=157
--counter-metric-count=277
--histogram-metric-count=28
--histogram-metric-bucket-count=10
--native-histogram-metric-count=0
--summary-metric-count=47
--summary-metric-objective-count=2
--series-count=10
--value-interval=300 # Changes values every 5m.
--series-interval=3600 # 1h series churn.
--metric-interval=0
This should give the total adjusted series to: 9860
```
