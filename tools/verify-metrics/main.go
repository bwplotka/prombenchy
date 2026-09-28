package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
)

type MetricType string

const (
	TypeGauge           MetricType = "GAUGE"
	TypeCounter         MetricType = "COUNTER"
	TypeHistogram       MetricType = "HISTOGRAM (classic)"
	TypeNativeHistogram MetricType = "HISTOGRAM (native)"
	TypeSummary         MetricType = "SUMMARY"
)

type MetricTarget struct {
	Type            MetricType `json:"type"`
	Name            string     `json:"name"`
	PromQLQueryName string     `json:"promql_query_name"`
	GCMType         string     `json:"gcm_type"`
}

// promQLSelector returns the `count(<name><matchers>)` expression used for
// point-in-time existence checks.
func (t MetricTarget) promQLSelector(matchersStr string) string {
	return fmt.Sprintf("count(%s%s)", t.PromQLQueryName, matchersStr)
}

// promQLRangeVector returns `<name><matchers>[<window>]` for range aggregations.
func (t MetricTarget) promQLRangeVector(matchersStr, windowStr string) string {
	return fmt.Sprintf("%s%s[%s]", t.PromQLQueryName, matchersStr, windowStr)
}

func formatPromQLDuration(d time.Duration) string {
	if d <= 0 {
		return "5m"
	}
	secs := int64(d.Round(time.Second).Seconds())
	if secs <= 0 {
		secs = 1
	}
	if secs%60 == 0 {
		return fmt.Sprintf("%dm", secs/60)
	}
	return fmt.Sprintf("%ds", secs)
}

type AvalancheConfig struct {
	GaugeMetricCount            int
	CounterMetricCount          int
	HistogramMetricCount        int
	HistogramMetricBucketCount  int
	NativeHistogramMetricCount  int
	SummaryMetricCount          int
	SummaryMetricObjectiveCount int
	SeriesCount                 int
	MetricNameLength            int
	MetricInterval              int
	MetricCycle                 int
}

func DefaultAvalancheConfig() AvalancheConfig {
	return AvalancheConfig{
		GaugeMetricCount:            1,
		CounterMetricCount:          1,
		HistogramMetricCount:        1,
		HistogramMetricBucketCount:  10,
		NativeHistogramMetricCount:  1,
		SummaryMetricCount:          1,
		SummaryMetricObjectiveCount: 2,
		SeriesCount:                 1,
		MetricNameLength:            5,
		MetricInterval:              0,
		MetricCycle:                 0,
	}
}

func ParseAvalancheArgs(args []string) AvalancheConfig {
	cfg := DefaultAvalancheConfig()
	var (
		hasGauge      bool
		hasCounter    bool
		hasHist       bool
		hasNativeHist bool
		hasSummary    bool
	)

	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		arg = strings.Trim(arg, `"'`)
		arg = strings.TrimPrefix(arg, "-")
		arg = strings.TrimPrefix(arg, "-")

		parts := strings.SplitN(arg, "=", 2)
		key := parts[0]
		val := ""
		if len(parts) > 1 {
			val = parts[1]
		}

		switch key {
		case "gauge-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.GaugeMetricCount = n
				hasGauge = true
			}
		case "counter-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.CounterMetricCount = n
				hasCounter = true
			}
		case "histogram-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.HistogramMetricCount = n
				hasHist = true
			}
		case "histogram-metric-bucket-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.HistogramMetricBucketCount = n
			}
		case "native-histogram-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.NativeHistogramMetricCount = n
				hasNativeHist = true
			}
		case "summary-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.SummaryMetricCount = n
				hasSummary = true
			}
		case "summary-metric-objective-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.SummaryMetricObjectiveCount = n
			}
		case "series-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.SeriesCount = n
			}
		case "metricname-length":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.MetricNameLength = n
			}
		case "metric-interval":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.MetricInterval = n
			}
		}
	}

	// If any metric count flag was explicitly provided, zero out the others
	// that were not explicitly specified.
	if hasGauge || hasCounter || hasHist || hasNativeHist || hasSummary {
		if !hasGauge {
			cfg.GaugeMetricCount = 0
		}
		if !hasCounter {
			cfg.CounterMetricCount = 0
		}
		if !hasHist {
			cfg.HistogramMetricCount = 0
		}
		if !hasNativeHist {
			cfg.NativeHistogramMetricCount = 0
		}
		if !hasSummary {
			cfg.SummaryMetricCount = 0
		}
	}

	return cfg
}

func ExtractArgsFromManifest(filePath string) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest file %s: %w", filePath, err)
	}

	// Extract lines containing avalanche arguments e.g. - "--gauge-metric-count=1"
	re := regexp.MustCompile(`--[a-zA-Z0-9_-]+=[^ \n\r"']+`)
	matches := re.FindAllString(string(data), -1)
	return matches, nil
}

func GetDeploymentArgsFromK8s(ctx context.Context, namespace string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "kubectl", "get", "deployment", "avalanche", "-n", namespace, "-o", "jsonpath={.spec.template.spec.containers[0].args}")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl get deployment avalanche failed: %w", err)
	}

	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "[]" {
		return nil, fmt.Errorf("no container args found in deployment avalanche in namespace %s", namespace)
	}

	var args []string
	if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
		// Try space separated or line separated
		args = strings.Fields(trimmed)
	}
	return args, nil
}

func GenerateMetrics(cfg AvalancheConfig) []MetricTarget {
	var metrics []MetricTarget
	length := cfg.MetricNameLength
	if length <= 0 {
		length = 5
	}
	mPrefix := strings.Repeat("m", length)

	// Gauges
	for id := 0; id < cfg.GaugeMetricCount; id++ {
		name := fmt.Sprintf("avalanche_gauge_metric_%s_%d_%d", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeGauge,
			Name:            name,
			PromQLQueryName: name,
			GCMType:         fmt.Sprintf("prometheus.googleapis.com/%s/gauge", name),
		})
	}

	// Counters
	for id := 0; id < cfg.CounterMetricCount; id++ {
		name := fmt.Sprintf("avalanche_counter_metric_%s_%d_%d_total", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeCounter,
			Name:            name,
			PromQLQueryName: name,
			GCMType:         fmt.Sprintf("prometheus.googleapis.com/%s/counter", name),
		})
	}

	// Histograms (classic)
	for id := 0; id < cfg.HistogramMetricCount; id++ {
		name := fmt.Sprintf("avalanche_histogram_metric_%s_%d_%d", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeHistogram,
			Name:            name,
			PromQLQueryName: fmt.Sprintf("%s_count", name),
			GCMType:         fmt.Sprintf("prometheus.googleapis.com/%s/histogram", name),
		})
	}

	// Native Histograms.
	//
	// The current GCM API does not support querying native histograms as such:
	// an ingested native histogram is a distribution, which the GMP PromQL API
	// exposes only as <name>_count / _sum / _bucket. Querying the bare family
	// name always yields an empty result, so _count is the name to verify.
	for id := 0; id < cfg.NativeHistogramMetricCount; id++ {
		name := fmt.Sprintf("avalanche_native_histogram_metric_%s_%d_%d", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeNativeHistogram,
			Name:            name,
			PromQLQueryName: fmt.Sprintf("%s_count", name),
			GCMType:         fmt.Sprintf("prometheus.googleapis.com/%s/histogram", name),
		})
	}

	// Summaries
	for id := 0; id < cfg.SummaryMetricCount; id++ {
		name := fmt.Sprintf("avalanche_summary_metric_%s_%d_%d", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeSummary,
			Name:            name,
			PromQLQueryName: fmt.Sprintf("%s_count", name),
			GCMType:         fmt.Sprintf("prometheus.googleapis.com/%s/summary", name),
		})
	}

	return metrics
}

func getAuthToken(ctx context.Context, explicitToken string) (string, error) {
	if explicitToken != "" {
		return explicitToken, nil
	}
	if envToken := os.Getenv("BEARER_TOKEN"); envToken != "" {
		return envToken, nil
	}
	// Use gcloud to acquire an OAuth2 auth token for Google Cloud APIs.
	// Try 'gcloud auth application-default print-access-token' first as it returns a cloud-platform scoped
	// OAuth2 token suitable for Monitoring & Managed Prometheus APIs.
	// Fall back to 'gcloud auth print-access-token'.
	for _, args := range [][]string{
		{"auth", "application-default", "print-access-token"},
		{"auth", "print-access-token"},
	} {
		cmd := exec.CommandContext(ctx, "gcloud", args...)
		out, err := cmd.Output()
		if err == nil {
			tok := strings.TrimSpace(string(out))
			if tok != "" {
				return tok, nil
			}
		}
	}
	return "", fmt.Errorf("no GCP auth token found via gcloud; run 'gcloud auth application-default login' (or 'gcloud auth login') or pass -token")
}

func getProjectID(explicitProject string) string {
	if explicitProject != "" {
		return explicitProject
	}
	if envProject := os.Getenv("PROJECT_ID"); envProject != "" {
		return envProject
	}
	out, err := exec.Command("gcloud", "config", "get", "project").Output()
	if err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" {
			return p
		}
	}
	return ""
}

type MetricResult struct {
	Target        MetricTarget `json:"target"`
	Found         bool         `json:"found"`
	SeriesCount   int          `json:"series_count"`
	SampleCount   int          `json:"sample_count"`
	MinVal        float64      `json:"min_val"`
	MaxVal        float64      `json:"max_val"`
	HasStats      bool         `json:"has_stats,omitempty"`
	ValidationErr string       `json:"validation_error,omitempty"`
	Err           string       `json:"error,omitempty"`
}

// validateMetricOverWindow assesses whether a metric's over-time data in the
// queried window matches expectations for the single-series acceptance test:
//  1. SeriesCount matches expectedSeries (when expectedSeries > 0).
//  2. SampleCount >= 2 over the window.
//  3. Value progression matches MetricType semantics:
//     - GAUGE: 0 <= MinVal <= MaxVal <= 100
//     - COUNTER, HISTOGRAM (classic), HISTOGRAM (native), SUMMARY:
//     MinVal >= 0 and MaxVal > MinVal (monotonically increasing over time).
func validateMetricOverWindow(r *MetricResult, expectedSeries int) {
	if !r.Found || !r.HasStats {
		return
	}
	var reasons []string
	if expectedSeries > 0 && r.SeriesCount != expectedSeries {
		reasons = append(reasons, fmt.Sprintf("expected %d series, got %d", expectedSeries, r.SeriesCount))
	}
	if r.SampleCount < 2 {
		reasons = append(reasons, fmt.Sprintf("expected >= 2 samples in window, got %d", r.SampleCount))
	} else {
		switch r.Target.Type {
		case TypeGauge:
			if r.MinVal < 0 || r.MaxVal > 100 || r.MaxVal < r.MinVal {
				reasons = append(reasons, fmt.Sprintf("gauge values out of expected [0, 100] range (min=%.2f, max=%.2f)", r.MinVal, r.MaxVal))
			}
		case TypeCounter, TypeHistogram, TypeNativeHistogram, TypeSummary:
			if r.MinVal < 0 {
				reasons = append(reasons, fmt.Sprintf("cumulative metric has negative min=%.2f", r.MinVal))
			}
			if r.MaxVal <= r.MinVal {
				reasons = append(reasons, fmt.Sprintf("cumulative metric did not increase over window (min=%.2f, max=%.2f)", r.MinVal, r.MaxVal))
			}
		}
	}
	if len(reasons) > 0 {
		r.ValidationErr = strings.Join(reasons, "; ")
	}
}

type VerificationSummary struct {
	Mode          string `json:"mode"`
	Window        string `json:"window,omitempty"`
	TotalExpected int    `json:"total_expected"`
	TotalFound    int    `json:"total_found"`
	// TotalMissing counts metrics the backend answered about successfully but
	// had no data for (or failed window validation). These are genuine gaps.
	TotalMissing int `json:"total_missing"`
	// TotalErrored counts metrics whose status could not be determined because
	// the query itself failed (quota exhaustion, timeouts, backend errors).
	// These are NOT evidence of missing data.
	TotalErrored   int                     `json:"total_errored"`
	DurationMs     int64                   `json:"duration_ms"`
	Passed         bool                    `json:"passed"`
	Inconclusive   bool                    `json:"inconclusive"`
	ByType         map[MetricType]TypeStat `json:"by_type"`
	MissingMetrics []string                `json:"missing_metrics,omitempty"`
	InvalidMetrics []string                `json:"invalid_metrics,omitempty"`
	ErroredMetrics []string                `json:"errored_metrics,omitempty"`
	// ErrorSamples maps a representative error message to how many metrics hit it.
	ErrorSamples map[string]int `json:"error_samples,omitempty"`
}

type TypeStat struct {
	Expected    int     `json:"expected"`
	Found       int     `json:"found"`
	Missing     int     `json:"missing"`
	Errored     int     `json:"errored"`
	SeriesCount int     `json:"series_count,omitempty"`
	SampleCount int     `json:"sample_count,omitempty"`
	MinVal      float64 `json:"min_val,omitempty"`
	MaxVal      float64 `json:"max_val,omitempty"`
	HasStats    bool    `json:"has_stats,omitempty"`
}

// PromQL response structures
type promQLResponse struct {
	Status    string `json:"status"`
	ErrorType string `json:"errorType"`
	Error     string `json:"error"`
	Data      struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Value  []interface{}     `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func buildLabelMatchers(namespace, cluster, customMatchers string) string {
	var matchers []string
	if namespace != "" {
		matchers = append(matchers, fmt.Sprintf(`namespace="%s"`, namespace))
	}
	if cluster != "" {
		matchers = append(matchers, fmt.Sprintf(`cluster="%s"`, cluster))
	}
	if customMatchers != "" {
		customMatchers = strings.TrimPrefix(customMatchers, "{")
		customMatchers = strings.TrimSuffix(customMatchers, "}")
		for _, m := range strings.Split(customMatchers, ",") {
			m = strings.TrimSpace(m)
			if m != "" {
				matchers = append(matchers, m)
			}
		}
	}
	if len(matchers) == 0 {
		return ""
	}
	return "{" + strings.Join(matchers, ", ") + "}"
}

// defaultMaxRetries is the number of additional attempts made for retryable
// responses (HTTP 429 and 5xx).
const defaultMaxRetries = 5

// isRetryableStatus reports whether an HTTP status is worth retrying.
func isRetryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// backoffDelay returns an exponentially growing delay with jitter, capped at 15s.
func backoffDelay(attempt int) time.Duration {
	d := time.Duration(500*math.Pow(2, float64(attempt))) * time.Millisecond
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	// Up to 25% jitter so parallel workers do not retry in lockstep.
	jitter := time.Duration(rand.Int63n(int64(d/4) + 1))
	return d + jitter
}

// doQueryWithRetry POSTs a PromQL query and retries retryable failures with
// exponential backoff. It returns the response body on success.
func doQueryWithRetry(ctx context.Context, client *http.Client, endpoint, token, projectID, query string, maxRetries int) ([]byte, error) {
	data := url.Values{}
	data.Set("query", query)

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := backoffDelay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if projectID != "" {
			req.Header.Set("X-Goog-User-Project", projectID)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		if !isRetryableStatus(resp.StatusCode) {
			return nil, lastErr
		}
		// Prefer the server's own pacing hint when it provides one.
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, convErr := strconv.Atoi(ra); convErr == nil && secs > 0 {
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
				case <-time.After(time.Duration(secs) * time.Second):
				}
			}
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxRetries+1, lastErr)
}

type metricWindowStats struct {
	found       bool
	hasStats    bool
	seriesCount int
	sampleCount int
	minVal      float64
	maxVal      float64
}

func buildPromQLWindowExpr(t MetricTarget, matchersStr, windowStr string) string {
	rv := t.promQLRangeVector(matchersStr, windowStr)
	return fmt.Sprintf(
		`label_replace(label_replace(count_over_time(%s), "metric", "%s", "", ""), "__stat__", "count", "", "") or label_replace(label_replace(min_over_time(%s), "metric", "%s", "", ""), "__stat__", "min", "", "") or label_replace(label_replace(max_over_time(%s), "metric", "%s", "", ""), "__stat__", "max", "", "")`,
		rv, t.Name, rv, t.Name, rv, t.Name,
	)
}

func verifyPromQL(ctx context.Context, client *http.Client, endpoint, token, projectID string, targets []MetricTarget, matchersStr string, window time.Duration, expectedSeries, batchSize, concurrency, maxRetries int) map[string]MetricResult {
	results := make(map[string]MetricResult, len(targets))
	for _, t := range targets {
		results[t.Name] = MetricResult{Target: t, Found: false}
	}

	if batchSize <= 0 {
		batchSize = 10
	}
	if concurrency <= 0 {
		concurrency = 5
	}
	windowStr := formatPromQLDuration(window)

	var batches [][]MetricTarget
	for i := 0; i < len(targets); i += batchSize {
		end := i + batchSize
		if end > len(targets) {
			end = len(targets)
		}
		batches = append(batches, targets[i:end])
	}

	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, b := range batches {
		batch := b
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			var parts []string
			for _, t := range batch {
				parts = append(parts, buildPromQLWindowExpr(t, matchersStr, windowStr))
			}
			query := strings.Join(parts, " or ")

			statsInBatch, err := executePromQLWindow(ctx, client, endpoint, token, projectID, query, maxRetries)
			if err != nil {
				// Batch failed; fall back to checking each metric in the batch individually.
				for _, t := range batch {
					singleQuery := buildPromQLWindowExpr(t, matchersStr, windowStr)
					stMap, sErr := executePromQLWindow(ctx, client, endpoint, token, projectID, singleQuery, maxRetries)
					mu.Lock()
					r := results[t.Name]
					if sErr != nil {
						r.Err = sErr.Error()
					} else if st, ok := stMap[t.Name]; ok && st.found {
						r.Found = true
						r.HasStats = st.hasStats
						r.SeriesCount = st.seriesCount
						r.SampleCount = st.sampleCount
						r.MinVal = st.minVal
						r.MaxVal = st.maxVal
						validateMetricOverWindow(&r, expectedSeries)
					}
					results[t.Name] = r
					mu.Unlock()
				}
				return
			}

			mu.Lock()
			for _, t := range batch {
				r := results[t.Name]
				if st, ok := statsInBatch[t.Name]; ok && st.found {
					r.Found = true
					r.HasStats = st.hasStats
					r.SeriesCount = st.seriesCount
					r.SampleCount = st.sampleCount
					r.MinVal = st.minVal
					r.MaxVal = st.maxVal
					validateMetricOverWindow(&r, expectedSeries)
				}
				r.Err = ""
				results[t.Name] = r
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

func parsePromQLSampleValue(v []interface{}) (float64, bool) {
	if len(v) < 2 {
		return 0, false
	}
	switch val := v[1].(type) {
	case string:
		f, err := strconv.ParseFloat(val, 64)
		return f, err == nil
	case float64:
		return val, true
	default:
		return 0, false
	}
}

func executePromQLWindow(ctx context.Context, client *http.Client, endpoint, token, projectID, query string, maxRetries int) (map[string]metricWindowStats, error) {
	body, err := doQueryWithRetry(ctx, client, endpoint, token, projectID, query, maxRetries)
	if err != nil {
		return nil, err
	}

	var pResp promQLResponse
	if err := json.Unmarshal(body, &pResp); err != nil {
		return nil, err
	}
	if pResp.Status != "success" {
		return nil, fmt.Errorf("PromQL error: %s: %s", pResp.ErrorType, pResp.Error)
	}

	stats := make(map[string]metricWindowStats)
	hasMin := make(map[string]bool)
	hasMax := make(map[string]bool)

	for _, r := range pResp.Data.Result {
		metricName, ok := r.Metric["metric"]
		if !ok || metricName == "" {
			continue
		}
		st := stats[metricName]
		st.found = true

		statKind := r.Metric["__stat__"]
		val, hasVal := parsePromQLSampleValue(r.Value)

		switch statKind {
		case "count":
			st.hasStats = true
			st.seriesCount++
			if hasVal {
				st.sampleCount += int(math.Round(val))
			}
		case "min":
			st.hasStats = true
			if hasVal && (!hasMin[metricName] || val < st.minVal) {
				st.minVal = val
				hasMin[metricName] = true
			}
		case "max":
			st.hasStats = true
			if hasVal && (!hasMax[metricName] || val > st.maxVal) {
				st.maxVal = val
				hasMax[metricName] = true
			}
		default:
			// Fallback for simple mock responses without __stat__.
			st.seriesCount++
			if hasVal {
				st.sampleCount += int(math.Round(val))
			}
		}
		stats[metricName] = st
	}
	return stats, nil
}

// doGetWithRetry issues a GET against a Google Cloud API and retries retryable
// failures with the same backoff policy as doQueryWithRetry.
func doGetWithRetry(ctx context.Context, client *http.Client, reqURL, token, projectID string, maxRetries int) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
			case <-time.After(backoffDelay(attempt - 1)):
			}
		}

		req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if projectID != "" {
			req.Header.Set("X-Goog-User-Project", projectID)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}

		if resp.StatusCode == http.StatusOK {
			return body, nil
		}

		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		if !isRetryableStatus(resp.StatusCode) {
			return nil, lastErr
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxRetries+1, lastErr)
}

// GCM response structures
type gcmPointValue struct {
	DoubleValue       *float64 `json:"doubleValue"`
	Int64Value        *string  `json:"int64Value"`
	DistributionValue *struct {
		Count string  `json:"count"`
		Mean  float64 `json:"mean"`
	} `json:"distributionValue"`
}

type gcmTimeSeriesResponse struct {
	TimeSeries []struct {
		Metric struct {
			Type   string            `json:"type"`
			Labels map[string]string `json:"labels"`
		} `json:"metric"`
		Points []struct {
			Value gcmPointValue `json:"value"`
		} `json:"points"`
	} `json:"timeSeries"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func extractGCMPointNumeric(v gcmPointValue) (float64, bool) {
	if v.DoubleValue != nil {
		return *v.DoubleValue, true
	}
	if v.Int64Value != nil {
		if f, err := strconv.ParseFloat(*v.Int64Value, 64); err == nil {
			return f, true
		}
	}
	if v.DistributionValue != nil && v.DistributionValue.Count != "" {
		if f, err := strconv.ParseFloat(v.DistributionValue.Count, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

func verifyGCM(ctx context.Context, client *http.Client, projectID, token string, isStaging bool, targets []MetricTarget, namespace, cluster string, window time.Duration, expectedSeries, concurrency, maxRetries int) map[string]MetricResult {
	results := make(map[string]MetricResult, len(targets))
	for _, t := range targets {
		results[t.Name] = MetricResult{Target: t, Found: false}
	}

	if concurrency <= 0 {
		concurrency = 10
	}

	now := time.Now().UTC()
	startTime := now.Add(-window).Format(time.RFC3339)
	endTime := now.Format(time.RFC3339)

	var mu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	host := "monitoring.googleapis.com"
	if isStaging {
		host = "staging-monitoring.sandbox.googleapis.com"
	}
	endpoint := fmt.Sprintf("https://%s/v3/projects/%s/timeSeries", host, projectID)

	for _, t := range targets {
		target := t
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			filter := fmt.Sprintf(`metric.type = "%s"`, target.GCMType)
			if namespace != "" {
				filter += fmt.Sprintf(` AND resource.labels.namespace = "%s"`, namespace)
			}
			if cluster != "" {
				filter += fmt.Sprintf(` AND resource.labels.cluster = "%s"`, cluster)
			}

			params := url.Values{}
			params.Set("filter", filter)
			params.Set("interval.startTime", startTime)
			params.Set("interval.endTime", endTime)
			params.Set("view", "FULL")

			reqURL := endpoint + "?" + params.Encode()
			body, err := doGetWithRetry(ctx, client, reqURL, token, projectID, maxRetries)
			if err != nil {
				mu.Lock()
				r := results[target.Name]
				r.Err = err.Error()
				results[target.Name] = r
				mu.Unlock()
				return
			}

			var gResp gcmTimeSeriesResponse
			if err := json.Unmarshal(body, &gResp); err != nil {
				mu.Lock()
				r := results[target.Name]
				r.Err = err.Error()
				results[target.Name] = r
				mu.Unlock()
				return
			}

			mu.Lock()
			r := results[target.Name]
			if gResp.Error != nil {
				r.Err = fmt.Sprintf("GCM error %d: %s", gResp.Error.Code, gResp.Error.Message)
			} else if len(gResp.TimeSeries) > 0 {
				r.Found = true
				r.SeriesCount = len(gResp.TimeSeries)
				var hasVal bool
				for _, ts := range gResp.TimeSeries {
					r.SampleCount += len(ts.Points)
					for _, pt := range ts.Points {
						if val, ok := extractGCMPointNumeric(pt.Value); ok {
							if !hasVal {
								r.MinVal = val
								r.MaxVal = val
								hasVal = true
							} else {
								if val < r.MinVal {
									r.MinVal = val
								}
								if val > r.MaxVal {
									r.MaxVal = val
								}
							}
						}
					}
				}
				if hasVal {
					r.HasStats = true
					// For GCM summary quantile metric, each objective quantile is a separate series
					expSeries := expectedSeries
					if target.Type == TypeSummary {
						expSeries = 0
					}
					validateMetricOverWindow(&r, expSeries)
				}
			}
			results[target.Name] = r
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

// summarizeResults aggregates per-metric outcomes into three buckets: found & valid,
// missing/invalid (backend answered, no data or failed window validation) and errored
// (query failed, status unknown).
func summarizeResults(mode string, results map[string]MetricResult, duration time.Duration) VerificationSummary {
	summary := VerificationSummary{
		Mode:          mode,
		TotalExpected: len(results),
		DurationMs:    duration.Milliseconds(),
		ByType:        make(map[MetricType]TypeStat),
		ErrorSamples:  make(map[string]int),
	}

	for _, r := range results {
		stat := summary.ByType[r.Target.Type]
		stat.Expected++
		if r.HasStats {
			stat.HasStats = true
			stat.SeriesCount += r.SeriesCount
			stat.SampleCount += r.SampleCount
			stat.MinVal = r.MinVal
			stat.MaxVal = r.MaxVal
		}
		switch {
		case r.Found && r.ValidationErr == "":
			stat.Found++
			summary.TotalFound++
		case r.Err != "":
			stat.Errored++
			summary.TotalErrored++
			summary.ErroredMetrics = append(summary.ErroredMetrics, r.Target.Name)
			summary.ErrorSamples[summarizeErr(r.Err)]++
		case r.Found && r.ValidationErr != "":
			stat.Missing++
			summary.TotalMissing++
			summary.InvalidMetrics = append(summary.InvalidMetrics, fmt.Sprintf("%s (%s)", r.Target.Name, r.ValidationErr))
		default:
			stat.Missing++
			summary.TotalMissing++
			summary.MissingMetrics = append(summary.MissingMetrics, r.Target.Name)
		}
		summary.ByType[r.Target.Type] = stat
	}

	sort.Strings(summary.MissingMetrics)
	sort.Strings(summary.InvalidMetrics)
	sort.Strings(summary.ErroredMetrics)
	if len(summary.ErrorSamples) == 0 {
		summary.ErrorSamples = nil
	}
	summary.Inconclusive = summary.TotalErrored > 0
	summary.Passed = summary.TotalMissing == 0 && summary.TotalErrored == 0 && summary.TotalExpected > 0
	return summary
}

func summarizeErr(err string) string {
	err = strings.Join(strings.Fields(err), " ")
	const maxLen = 160
	if len(err) > maxLen {
		err = err[:maxLen] + "..."
	}
	return err
}

func printSummaryTable(w io.Writer, s VerificationSummary) {
	windowLabel := ""
	if s.Window != "" {
		windowLabel = fmt.Sprintf(", Window: %s", s.Window)
	}
	fmt.Fprintf(w, "\n--- Metric Verification Summary (Mode: %s%s, Duration: %.2fs) ---\n", s.Mode, windowLabel, float64(s.DurationMs)/1000.0)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "METRIC TYPE\tEXPECTED\tQUERYABLE\tSERIES\tSAMPLES\tMIN..MAX\tMISSING\tERRORED\tSTATUS")

	types := []MetricType{TypeGauge, TypeCounter, TypeHistogram, TypeNativeHistogram, TypeSummary}
	for _, t := range types {
		stat, ok := s.ByType[t]
		if !ok || stat.Expected == 0 {
			continue
		}
		status := "OK"
		switch {
		case stat.Missing > 0:
			status = "MISSING"
		case stat.Errored > 0:
			status = "INCONCLUSIVE"
		}
		minMaxStr := "-"
		if stat.HasStats {
			minMaxStr = fmt.Sprintf("%.1f..%.1f", stat.MinVal, stat.MaxVal)
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t%d\t%d\t%s\n",
			t, stat.Expected, stat.Found, stat.SeriesCount, stat.SampleCount, minMaxStr, stat.Missing, stat.Errored, status)
	}

	fmt.Fprintln(tw, "--------------------------------------------------------")
	totalStatus := "PASSED"
	switch {
	case s.TotalMissing > 0:
		totalStatus = "FAILED"
	case s.TotalErrored > 0:
		totalStatus = "INCONCLUSIVE"
	}
	fmt.Fprintf(tw, "TOTAL\t%d\t%d\t-\t-\t-\t%d\t%d\t%s\n", s.TotalExpected, s.TotalFound, s.TotalMissing, s.TotalErrored, totalStatus)
	_ = tw.Flush()

	printList := func(title string, items []string) {
		if len(items) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s (%d):\n", title, len(items))
		const maxShow = 25
		for i, m := range items {
			if i >= maxShow {
				fmt.Fprintf(w, "  ... and %d more\n", len(items)-maxShow)
				break
			}
			fmt.Fprintf(w, "  - %s\n", m)
		}
	}

	printList("Missing / Unqueryable metrics", s.MissingMetrics)
	printList("Window validation failures", s.InvalidMetrics)

	if s.TotalErrored > 0 {
		fmt.Fprintf(w, "\nWARNING: %d metric(s) could not be verified because the query failed.\n", s.TotalErrored)
		fmt.Fprintf(w, "These are NOT reported as missing data - their state is unknown.\n")
		fmt.Fprintf(w, "If these are quota errors (HTTP 429), lower -concurrency or raise -max-retries.\n")
		type kv struct {
			msg string
			n   int
		}
		var kvs []kv
		for m, n := range s.ErrorSamples {
			kvs = append(kvs, kv{m, n})
		}
		sort.Slice(kvs, func(i, j int) bool {
			if kvs[i].n != kvs[j].n {
				return kvs[i].n > kvs[j].n
			}
			return kvs[i].msg < kvs[j].msg
		})
		fmt.Fprintf(w, "\nErrors by kind:\n")
		for i, e := range kvs {
			if i >= 5 {
				fmt.Fprintf(w, "  ... and %d more kind(s)\n", len(kvs)-5)
				break
			}
			fmt.Fprintf(w, "  %5dx %s\n", e.n, e.msg)
		}
		printList("Unverified metrics", s.ErroredMetrics)
	}
	fmt.Fprintln(w)
}

func detectStagingFromScenario(scenarioDir string) bool {
	if scenarioDir == "" {
		return false
	}
	var found bool
	_ = filepath.Walk(scenarioDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			data, readErr := os.ReadFile(path)
			if readErr == nil && (strings.Contains(string(data), "staging-monitoring") || strings.Contains(string(data), "sandbox.googleapis.com")) {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

func main() {
	benchName := flag.String("bench-name", "", "Scenario / benchmark name (used as namespace label matcher).")
	namespace := flag.String("namespace", "", "Metric namespace label filter (defaults to bench-name).")
	avalancheNamespace := flag.String("avalanche-namespace", "core", "Kubernetes namespace where the shared avalanche deployment runs.")
	clusterName := flag.String("cluster-name", "", "Cluster name to filter metrics.")
	projectID := flag.String("project-id", "", "Google Cloud project ID (defaults to gcloud current project).")
	mode := flag.String("mode", "promql", "Verification mode: 'promql', 'gcm', or 'both'.")
	staging := flag.Bool("staging", false, "Use staging Google Cloud Monitoring endpoint (staging-monitoring.sandbox.googleapis.com).")
	promqlURL := flag.String("promql-url", "", "PromQL query endpoint URL. Defaults to GCP Managed Prometheus endpoint.")
	token := flag.String("token", "", "Bearer token for API authorization. Defaults to ADC/gcloud auth token.")
	scenarioDir := flag.String("scenario", "", "Scenario directory path (e.g. ./manifests/scenarios/gmp).")
	loadManifest := flag.String("load-manifest", "", "Path to avalanche YAML manifest. Defaults to ./manifests/core/3_avalanche.yaml.")
	matchers := flag.String("matchers", "", "Extra PromQL label matchers in format 'k=v,k2=v2'.")
	format := flag.String("format", "table", "Output format: 'table' or 'json'.")

	concurrency := flag.Int("concurrency", 4, "Number of concurrent workers for querying. Raising this risks HTTP 429 quota errors.")
	batchSize := flag.Int("batch-size", 10, "Number of metrics to batch in each PromQL union query.")
	maxRetries := flag.Int("max-retries", defaultMaxRetries, "Retries for throttled/transient query failures (HTTP 429 and 5xx), with exponential backoff.")
	timeout := flag.Duration("timeout", 300*time.Second, "Overall verification timeout.")
	window := flag.Duration("window", 5*time.Minute, "Time window over which to assess the 1 series data.")
	lookback := flag.Duration("lookback", 0, "Deprecated alias for -window.")

	// Explicit avalanche configuration overrides
	gaugeCount := flag.Int("gauge-metric-count", -1, "Override gauge metric count.")
	counterCount := flag.Int("counter-metric-count", -1, "Override counter metric count.")
	histogramCount := flag.Int("histogram-metric-count", -1, "Override classic histogram metric count.")
	nativeHistogramCount := flag.Int("native-histogram-metric-count", -1, "Override native histogram metric count.")
	summaryCount := flag.Int("summary-metric-count", -1, "Override summary metric count.")
	seriesCount := flag.Int("series-count", -1, "Override expected series count per metric.")
	metricLength := flag.Int("metricname-length", -1, "Override metricname length.")
	metricInterval := flag.Int("metric-interval", -1, "Override metric interval.")

	flag.Parse()

	evalWindow := *window
	if *lookback > 0 {
		evalWindow = *lookback
	}

	ns := *benchName
	if *namespace != "" {
		ns = *namespace
	}

	proj := getProjectID(*projectID)

	isStaging := *staging
	if !isStaging && *scenarioDir != "" && detectStagingFromScenario(*scenarioDir) {
		isStaging = true
		if *format != "json" {
			fmt.Println("Auto-detected staging environment from scenario manifests.")
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	// 1. Resolve Avalanche configuration
	var avalancheArgs []string

	// Attempt 1: From Kubernetes deployment in avalanche-namespace (core), or fallback to ns
	for _, candidateNS := range []string{*avalancheNamespace, ns} {
		if candidateNS == "" {
			continue
		}
		args, err := GetDeploymentArgsFromK8s(ctx, candidateNS)
		if err == nil && len(args) > 0 {
			avalancheArgs = args
			if *format != "json" {
				fmt.Printf("Loaded Avalanche configuration from Kubernetes deployment avalanche in namespace %s\n", candidateNS)
			}
			break
		}
	}

	// Attempt 2: From load manifest or core manifest
	if len(avalancheArgs) == 0 {
		manifestPath := *loadManifest
		if manifestPath == "" && *scenarioDir != "" {
			cand := filepath.Join(*scenarioDir, "avalanche.yaml")
			if _, err := os.Stat(cand); err == nil {
				manifestPath = cand
			}
		}
		if manifestPath == "" {
			for _, defaultPath := range []string{
				"./manifests/core/3_avalanche.yaml",
				"./manifests/load/avalanche.exampletarget.yaml",
			} {
				if _, err := os.Stat(defaultPath); err == nil {
					manifestPath = defaultPath
					break
				}
			}
		}
		if manifestPath != "" {
			args, err := ExtractArgsFromManifest(manifestPath)
			if err == nil && len(args) > 0 {
				avalancheArgs = args
				if *format != "json" {
					fmt.Printf("Loaded Avalanche configuration from manifest: %s\n", manifestPath)
				}
			}
		}
	}

	cfg := ParseAvalancheArgs(avalancheArgs)

	// Apply explicit flag overrides
	if *gaugeCount >= 0 {
		cfg.GaugeMetricCount = *gaugeCount
	}
	if *counterCount >= 0 {
		cfg.CounterMetricCount = *counterCount
	}
	if *histogramCount >= 0 {
		cfg.HistogramMetricCount = *histogramCount
	}
	if *nativeHistogramCount >= 0 {
		cfg.NativeHistogramMetricCount = *nativeHistogramCount
	}
	if *summaryCount >= 0 {
		cfg.SummaryMetricCount = *summaryCount
	}
	if *seriesCount >= 0 {
		cfg.SeriesCount = *seriesCount
	}
	if *metricLength >= 0 {
		cfg.MetricNameLength = *metricLength
	}
	if *metricInterval >= 0 {
		cfg.MetricInterval = *metricInterval
	}

	targets := GenerateMetrics(cfg)
	if len(targets) == 0 {
		log.Fatal("No metrics generated! Check avalanche arguments or pass flags (e.g. -gauge-metric-count=1).")
	}

	if *format != "json" {
		fmt.Printf("Expected %d metric families (%d series each) from Avalanche (gauges: %d, counters: %d, histograms: %d, native-histograms: %d, summaries: %d)\n",
			len(targets), cfg.SeriesCount, cfg.GaugeMetricCount, cfg.CounterMetricCount, cfg.HistogramMetricCount, cfg.NativeHistogramMetricCount, cfg.SummaryMetricCount)
	}

	// 2. Resolve Auth Token if needed
	var authToken string
	endpointPromQL := *promqlURL
	if endpointPromQL == "" {
		if proj != "" {
			host := "monitoring.googleapis.com"
			if isStaging {
				host = "staging-monitoring.sandbox.googleapis.com"
			}
			endpointPromQL = fmt.Sprintf("https://%s/v1/projects/%s/location/global/prometheus/api/v1/query", host, proj)
		} else {
			endpointPromQL = "http://localhost:19090/api/v1/query"
		}
	}

	needsGCPAuth := strings.Contains(endpointPromQL, "googleapis.com") || *mode == "gcm" || *mode == "both" || isStaging
	if needsGCPAuth {
		tok, err := getAuthToken(ctx, *token)
		if err != nil && *token == "" {
			log.Printf("Warning: failed to acquire GCP auth token: %v", err)
		} else {
			authToken = tok
		}
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	matchersStr := buildLabelMatchers(ns, *clusterName, *matchers)

	var anyMissing, anyErrored bool

	// 3. Execute PromQL verification
	if *mode == "promql" || *mode == "both" {
		if *format != "json" {
			stagingLabel := ""
			if isStaging {
				stagingLabel = " [staging]"
			}
			fmt.Printf("Verifying %d metrics over window %s via PromQL%s at %s with selector %s ...\n",
				len(targets), formatPromQLDuration(evalWindow), stagingLabel, endpointPromQL, matchersStr)
		}
		start := time.Now()
		promResults := verifyPromQL(ctx, httpClient, endpointPromQL, authToken, proj, targets, matchersStr, evalWindow, cfg.SeriesCount, *batchSize, *concurrency, *maxRetries)
		elapsed := time.Since(start)

		summary := summarizeResults("PromQL", promResults, elapsed)
		summary.Window = formatPromQLDuration(evalWindow)
		if *format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(summary)
		} else {
			printSummaryTable(os.Stdout, summary)
		}
		if summary.TotalMissing > 0 {
			anyMissing = true
		}
		if summary.TotalErrored > 0 {
			anyErrored = true
		}
	}

	// 4. Execute GCM verification
	if *mode == "gcm" || *mode == "both" {
		if proj == "" {
			log.Fatal("Project ID is required for GCM verification (pass -project-id).")
		}
		if authToken == "" {
			log.Fatal("GCP Auth token is required for GCM verification (pass -token or run 'gcloud auth application-default login').")
		}

		if *format != "json" {
			stagingLabel := ""
			if isStaging {
				stagingLabel = " [staging]"
			}
			fmt.Printf("Verifying %d metrics over window %s via GCM TimeSeries API%s (project: %s, namespace: %s, cluster: %s) ...\n",
				len(targets), formatPromQLDuration(evalWindow), stagingLabel, proj, ns, *clusterName)
		}
		start := time.Now()
		gcmResults := verifyGCM(ctx, httpClient, proj, authToken, isStaging, targets, ns, *clusterName, evalWindow, cfg.SeriesCount, *concurrency, *maxRetries)
		elapsed := time.Since(start)

		summary := summarizeResults("GCM", gcmResults, elapsed)
		summary.Window = formatPromQLDuration(evalWindow)
		if *format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(summary)
		} else {
			printSummaryTable(os.Stdout, summary)
		}
		if summary.TotalMissing > 0 {
			anyMissing = true
		}
		if summary.TotalErrored > 0 {
			anyErrored = true
		}
	}

	switch {
	case anyMissing:
		os.Exit(1)
	case anyErrored:
		os.Exit(2)
	}
}
