package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestParseAvalancheArgs(t *testing.T) {
	args := []string{
		"--gauge-metric-count=158",
		"--counter-metric-count=280",
		"--histogram-metric-count=28",
		"--histogram-metric-bucket-count=10",
		"--native-histogram-metric-count=5",
		"--summary-metric-count=48",
		"--summary-metric-objective-count=2",
		"--series-count=10",
		"--metricname-length=7",
		"--metric-interval=0",
	}

	cfg := ParseAvalancheArgs(args)
	if cfg.GaugeMetricCount != 158 {
		t.Errorf("expected GaugeMetricCount 158, got %d", cfg.GaugeMetricCount)
	}
	if cfg.CounterMetricCount != 280 {
		t.Errorf("expected CounterMetricCount 280, got %d", cfg.CounterMetricCount)
	}
	if cfg.HistogramMetricCount != 28 {
		t.Errorf("expected HistogramMetricCount 28, got %d", cfg.HistogramMetricCount)
	}
	if cfg.HistogramMetricBucketCount != 10 {
		t.Errorf("expected HistogramMetricBucketCount 10, got %d", cfg.HistogramMetricBucketCount)
	}
	if cfg.NativeHistogramMetricCount != 5 {
		t.Errorf("expected NativeHistogramMetricCount 5, got %d", cfg.NativeHistogramMetricCount)
	}
	if cfg.SummaryMetricCount != 48 {
		t.Errorf("expected SummaryMetricCount 48, got %d", cfg.SummaryMetricCount)
	}
	if cfg.SummaryMetricObjectiveCount != 2 {
		t.Errorf("expected SummaryMetricObjectiveCount 2, got %d", cfg.SummaryMetricObjectiveCount)
	}
	if cfg.MetricNameLength != 7 {
		t.Errorf("expected MetricNameLength 7, got %d", cfg.MetricNameLength)
	}
	if cfg.MetricInterval != 0 {
		t.Errorf("expected MetricInterval 0, got %d", cfg.MetricInterval)
	}
}

func TestGenerateMetrics(t *testing.T) {
	cfg := AvalancheConfig{
		GaugeMetricCount:           2,
		CounterMetricCount:         2,
		HistogramMetricCount:       1,
		NativeHistogramMetricCount: 1,
		SummaryMetricCount:         1,
		MetricNameLength:           5,
		MetricInterval:             0,
		MetricCycle:                0,
	}

	metrics := GenerateMetrics(cfg)
	if len(metrics) != 7 {
		t.Fatalf("expected 7 metrics, got %d", len(metrics))
	}

	expectedGauges := []string{
		"avalanche_gauge_metric_mmmmm_0_0",
		"avalanche_gauge_metric_mmmmm_0_1",
	}
	for i, name := range expectedGauges {
		if metrics[i].Type != TypeGauge {
			t.Errorf("metric %d expected type GAUGE, got %s", i, metrics[i].Type)
		}
		if metrics[i].Name != name {
			t.Errorf("metric %d expected name %s, got %s", i, name, metrics[i].Name)
		}
		if metrics[i].PromQLQueryName != name {
			t.Errorf("metric %d expected promql %s, got %s", i, name, metrics[i].PromQLQueryName)
		}
		expectedGCM := "prometheus.googleapis.com/" + name + "/gauge"
		if metrics[i].GCMType != expectedGCM {
			t.Errorf("metric %d expected GCM type %s, got %s", i, expectedGCM, metrics[i].GCMType)
		}
	}

	// Counter check
	counter := metrics[2]
	if counter.Type != TypeCounter {
		t.Errorf("expected counter type, got %s", counter.Type)
	}
	if counter.Name != "avalanche_counter_metric_mmmmm_0_0_total" {
		t.Errorf("expected counter name with _total, got %s", counter.Name)
	}
	if counter.GCMType != "prometheus.googleapis.com/avalanche_counter_metric_mmmmm_0_0_total/counter" {
		t.Errorf("expected counter GCM type, got %s", counter.GCMType)
	}

	// Classic Histogram check
	hist := metrics[4]
	if hist.Type != TypeHistogram {
		t.Errorf("expected histogram type, got %s", hist.Type)
	}
	if hist.PromQLQueryName != "avalanche_histogram_metric_mmmmm_0_0_count" {
		t.Errorf("expected PromQL query name with _count, got %s", hist.PromQLQueryName)
	}
	if hist.GCMType != "prometheus.googleapis.com/avalanche_histogram_metric_mmmmm_0_0/histogram" {
		t.Errorf("expected histogram GCM type, got %s", hist.GCMType)
	}

	// Native Histogram check
	nativeHist := metrics[5]
	if nativeHist.Type != TypeNativeHistogram {
		t.Errorf("expected native histogram type, got %s", nativeHist.Type)
	}
	if nativeHist.PromQLQueryName != "avalanche_native_histogram_metric_mmmmm_0_0" {
		t.Errorf("expected native histogram PromQL name without _count, got %s", nativeHist.PromQLQueryName)
	}

	// Summary check
	summary := metrics[6]
	if summary.Type != TypeSummary {
		t.Errorf("expected summary type, got %s", summary.Type)
	}
	if summary.PromQLQueryName != "avalanche_summary_metric_mmmmm_0_0_count" {
		t.Errorf("expected summary PromQL name with _count, got %s", summary.PromQLQueryName)
	}
	if summary.GCMType != "prometheus.googleapis.com/avalanche_summary_metric_mmmmm_0_0/summary" {
		t.Errorf("expected summary GCM type, got %s", summary.GCMType)
	}
}

func TestBuildLabelMatchers(t *testing.T) {
	tests := []struct {
		namespace string
		cluster   string
		custom    string
		expected  string
	}{
		{"", "", "", ""},
		{"gmp", "", "", `{namespace="gmp"}`},
		{"gmp", "my-cluster", "", `{namespace="gmp", cluster="my-cluster"}`},
		{"gmp", "my-cluster", `app="avalanche"`, `{namespace="gmp", cluster="my-cluster", app="avalanche"}`},
	}

	for _, tt := range tests {
		got := buildLabelMatchers(tt.namespace, tt.cluster, tt.custom)
		if got != tt.expected {
			t.Errorf("buildLabelMatchers(%q, %q, %q) = %q, expected %q", tt.namespace, tt.cluster, tt.custom, got, tt.expected)
		}
	}
}

func TestSummarizeAndPrintTable(t *testing.T) {
	targetGauge := MetricTarget{Type: TypeGauge, Name: "gauge_0"}
	targetCounter := MetricTarget{Type: TypeCounter, Name: "counter_0"}

	results := map[string]MetricResult{
		"gauge_0":   {Target: targetGauge, Found: true},
		"counter_0": {Target: targetCounter, Found: false},
	}

	summary := summarizeResults("PromQL", results, 1500*time.Millisecond)
	if summary.TotalExpected != 2 {
		t.Errorf("expected 2 total, got %d", summary.TotalExpected)
	}
	if summary.TotalFound != 1 {
		t.Errorf("expected 1 found, got %d", summary.TotalFound)
	}
	if summary.TotalMissing != 1 {
		t.Errorf("expected 1 missing, got %d", summary.TotalMissing)
	}
	if summary.Passed {
		t.Errorf("expected Passed to be false")
	}

	var buf bytes.Buffer
	printSummaryTable(&buf, summary)
	out := buf.String()
	if !strings.Contains(out, "FAILED") {
		t.Errorf("expected output to contain FAILED, got:\n%s", out)
	}
	if !strings.Contains(out, "counter_0") {
		t.Errorf("expected output to list missing counter_0, got:\n%s", out)
	}
}
