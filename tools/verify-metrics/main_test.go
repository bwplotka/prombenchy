package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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

	// Native Histogram check.
	// The GCM API cannot query native histograms directly - an ingested native
	// histogram is a distribution, exposed only as <name>_count / _sum / _bucket.
	nativeHist := metrics[5]
	if nativeHist.Type != TypeNativeHistogram {
		t.Errorf("expected native histogram type, got %s", nativeHist.Type)
	}
	if nativeHist.PromQLQueryName != "avalanche_native_histogram_metric_mmmmm_0_0_count" {
		t.Errorf("expected native histogram PromQL name with _count, got %s", nativeHist.PromQLQueryName)
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

func TestDetectStagingFromScenario(t *testing.T) {
	// Test against real scenario prom-rw which contains staging-monitoring
	if !detectStagingFromScenario("../../manifests/scenarios/prom-rw") {
		t.Errorf("expected manifests/scenarios/prom-rw to be detected as staging")
	}

	// Test against prom-rw-m which also contains staging-monitoring
	if !detectStagingFromScenario("../../manifests/scenarios/prom-rw-m") {
		t.Errorf("expected manifests/scenarios/prom-rw-m to be detected as staging")
	}

	// Test against prom-rw-nost-unkn which also contains staging-monitoring
	if !detectStagingFromScenario("../../manifests/scenarios/prom-rw-nost-unkn") {
		t.Errorf("expected manifests/scenarios/prom-rw-nost-unkn to be detected as staging")
	}

	// Test against prom-rw-m-nost-unkn which also contains staging-monitoring
	if !detectStagingFromScenario("../../manifests/scenarios/prom-rw-m-nost-unkn") {
		t.Errorf("expected manifests/scenarios/prom-rw-m-nost-unkn to be detected as staging")
	}

	// Test against gmp which does not contain staging-monitoring
	if detectStagingFromScenario("../../manifests/scenarios/gmp") {
		t.Errorf("expected manifests/scenarios/gmp NOT to be detected as staging")
	}

	// Test against gmp3 which does not contain staging-monitoring
	if detectStagingFromScenario("../../manifests/scenarios/gmp3") {
		t.Errorf("expected manifests/scenarios/gmp3 NOT to be detected as staging")
	}
}

func TestPromQLSelector(t *testing.T) {
	nativeHist := MetricTarget{
		Name:            "nh_0",
		PromQLQueryName: "nh_0_count",
	}
	if got, want := nativeHist.promQLSelector(`{namespace="gmp"}`), `count(nh_0_count{namespace="gmp"})`; got != want {
		t.Errorf("promQLSelector() = %q, want %q", got, want)
	}

	plain := MetricTarget{Name: "g_0", PromQLQueryName: "g_0"}
	if got, want := plain.promQLSelector(""), "count(g_0)"; got != want {
		t.Errorf("promQLSelector() = %q, want %q", got, want)
	}
}

// TestSummarizeResultsSeparatesErroredFromMissing guards the regression that
// made a quota outage look like data loss: a metric whose query failed must be
// counted as errored/inconclusive, never as missing.
func TestSummarizeResultsSeparatesErroredFromMissing(t *testing.T) {
	results := map[string]MetricResult{
		"found_0":   {Target: MetricTarget{Type: TypeGauge, Name: "found_0"}, Found: true},
		"missing_0": {Target: MetricTarget{Type: TypeGauge, Name: "missing_0"}, Found: false},
		"errored_0": {Target: MetricTarget{Type: TypeCounter, Name: "errored_0"}, Found: false, Err: "HTTP 429: Resource has been exhausted"},
		"errored_1": {Target: MetricTarget{Type: TypeCounter, Name: "errored_1"}, Found: false, Err: "HTTP 429: Resource has been exhausted"},
	}

	s := summarizeResults("PromQL", results, time.Second)

	if s.TotalFound != 1 {
		t.Errorf("expected 1 found, got %d", s.TotalFound)
	}
	if s.TotalMissing != 1 {
		t.Errorf("expected 1 missing, got %d", s.TotalMissing)
	}
	if s.TotalErrored != 2 {
		t.Errorf("expected 2 errored, got %d", s.TotalErrored)
	}
	if len(s.MissingMetrics) != 1 || s.MissingMetrics[0] != "missing_0" {
		t.Errorf("errored metrics must not leak into MissingMetrics, got %v", s.MissingMetrics)
	}
	if !s.Inconclusive {
		t.Error("expected run to be marked inconclusive")
	}
	if s.Passed {
		t.Error("expected Passed to be false")
	}
	if n := s.ErrorSamples["HTTP 429: Resource has been exhausted"]; n != 2 {
		t.Errorf("expected identical errors to be grouped with count 2, got %d", n)
	}

	var buf bytes.Buffer
	printSummaryTable(&buf, s)
	out := buf.String()
	for _, want := range []string{"ERRORED", "could not be verified", "HTTP 429"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q, got:\n%s", want, out)
		}
	}
}

// A run with only errors must not claim anything is missing.
func TestSummarizeResultsAllErroredReportsNoMissing(t *testing.T) {
	results := map[string]MetricResult{
		"a": {Target: MetricTarget{Type: TypeGauge, Name: "a"}, Err: "HTTP 429"},
	}
	s := summarizeResults("PromQL", results, time.Second)
	if s.TotalMissing != 0 {
		t.Errorf("expected 0 missing when every query failed, got %d", s.TotalMissing)
	}
	var buf bytes.Buffer
	printSummaryTable(&buf, s)
	if out := buf.String(); !strings.Contains(out, "INCONCLUSIVE") {
		t.Errorf("expected INCONCLUSIVE verdict, got:\n%s", out)
	}
}

func TestIsRetryableStatus(t *testing.T) {
	for _, tc := range []struct {
		code int
		want bool
	}{
		{http.StatusOK, false},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusServiceUnavailable, true},
	} {
		if got := isRetryableStatus(tc.code); got != tc.want {
			t.Errorf("isRetryableStatus(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestDoQueryWithRetryRecoversFrom429(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"code":429,"message":"Resource has been exhausted"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`))
	}))
	defer srv.Close()

	body, err := doQueryWithRetry(context.Background(), srv.Client(), srv.URL, "", "", "up", 3)
	if err != nil {
		t.Fatalf("expected retry to succeed, got error: %v", err)
	}
	if !strings.Contains(string(body), "success") {
		t.Errorf("unexpected body: %s", body)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 attempts, got %d", got)
	}
}

func TestDoQueryWithRetryDoesNotRetryClientError(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad query"))
	}))
	defer srv.Close()

	if _, err := doQueryWithRetry(context.Background(), srv.Client(), srv.URL, "", "", "up", 3); err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("HTTP 400 must not be retried, got %d attempts", got)
	}
}

func TestBackoffDelayGrowsAndIsCapped(t *testing.T) {
	first := backoffDelay(0)
	second := backoffDelay(1)
	if first < 500*time.Millisecond {
		t.Errorf("first delay too small: %v", first)
	}
	if second < first {
		t.Errorf("expected growing backoff, got %v then %v", first, second)
	}
	if capped := backoffDelay(20); capped > 19*time.Second {
		t.Errorf("expected capped backoff, got %v", capped)
	}
}
