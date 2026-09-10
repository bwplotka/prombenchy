package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
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

type AvalancheConfig struct {
	GaugeMetricCount            int
	CounterMetricCount          int
	HistogramMetricCount        int
	HistogramMetricBucketCount  int
	NativeHistogramMetricCount  int
	SummaryMetricCount          int
	SummaryMetricObjectiveCount int
	MetricNameLength            int
	MetricInterval              int
	MetricCycle                 int
}

func DefaultAvalancheConfig() AvalancheConfig {
	return AvalancheConfig{
		GaugeMetricCount:            500,
		CounterMetricCount:          0,
		HistogramMetricCount:        0,
		HistogramMetricBucketCount:  7,
		NativeHistogramMetricCount:  0,
		SummaryMetricCount:          0,
		SummaryMetricObjectiveCount: 2,
		MetricNameLength:            5,
		MetricInterval:              0,
		MetricCycle:                 0,
	}
}

func ParseAvalancheArgs(args []string) AvalancheConfig {
	cfg := DefaultAvalancheConfig()
	hasCustomGauge := false

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
				hasCustomGauge = true
			}
		case "counter-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.CounterMetricCount = n
			}
		case "histogram-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.HistogramMetricCount = n
			}
		case "histogram-metric-bucket-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.HistogramMetricBucketCount = n
			}
		case "native-histogram-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.NativeHistogramMetricCount = n
			}
		case "summary-metric-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.SummaryMetricCount = n
			}
		case "summary-metric-objective-count":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.SummaryMetricObjectiveCount = n
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

	// If user configured any other metric count and didn't set gauge-metric-count,
	// default gauge to 0 rather than avalanche's legacy default of 500.
	if !hasCustomGauge && (cfg.CounterMetricCount > 0 || cfg.HistogramMetricCount > 0 ||
		cfg.NativeHistogramMetricCount > 0 || cfg.SummaryMetricCount > 0) {
		cfg.GaugeMetricCount = 0
	}

	return cfg
}

func ExtractArgsFromManifest(filePath string) ([]string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading manifest file %s: %w", filePath, err)
	}

	// Extract lines containing avalanche arguments e.g. - "--gauge-metric-count=158"
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

	// Native Histograms
	for id := 0; id < cfg.NativeHistogramMetricCount; id++ {
		name := fmt.Sprintf("avalanche_native_histogram_metric_%s_%d_%d", mPrefix, cfg.MetricCycle, id)
		metrics = append(metrics, MetricTarget{
			Type:            TypeNativeHistogram,
			Name:            name,
			PromQLQueryName: name,
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
	// Try gcloud auth application-default print-access-token
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "application-default", "print-access-token")
	out, err := cmd.Output()
	if err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok, nil
		}
	}
	// Fallback to gcloud auth print-access-token
	cmd = exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
	out, err = cmd.Output()
	if err == nil {
		tok := strings.TrimSpace(string(out))
		if tok != "" {
			return tok, nil
		}
	}
	return "", fmt.Errorf("no GCP auth token found; run 'gcloud auth application-default login' or pass -token")
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
	Target MetricTarget `json:"target"`
	Found  bool         `json:"found"`
	Err    string       `json:"error,omitempty"`
}

type VerificationSummary struct {
	Mode           string                  `json:"mode"`
	TotalExpected  int                     `json:"total_expected"`
	TotalFound     int                     `json:"total_found"`
	TotalMissing   int                     `json:"total_missing"`
	DurationMs     int64                   `json:"duration_ms"`
	Passed         bool                    `json:"passed"`
	ByType         map[MetricType]TypeStat `json:"by_type"`
	MissingMetrics []string                `json:"missing_metrics,omitempty"`
}

type TypeStat struct {
	Expected int `json:"expected"`
	Found    int `json:"found"`
	Missing  int `json:"missing"`
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

func verifyPromQL(ctx context.Context, client *http.Client, endpoint string, token string, projectID string, targets []MetricTarget, matchersStr string, batchSize int, concurrency int) map[string]MetricResult {
	results := make(map[string]MetricResult, len(targets))
	for _, t := range targets {
		results[t.Name] = MetricResult{Target: t, Found: false}
	}

	if batchSize <= 0 {
		batchSize = 25
	}
	if concurrency <= 0 {
		concurrency = 5
	}

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

			// Construct batch query
			var parts []string
			for _, t := range batch {
				parts = append(parts, fmt.Sprintf(`label_replace(count(%s%s), "metric", "%s", "", "")`, t.PromQLQueryName, matchersStr, t.Name))
			}
			query := strings.Join(parts, " or ")

			foundInBatch, err := executePromQL(ctx, client, endpoint, token, projectID, query)
			if err != nil {
				// Batch failed; fall back to checking each metric in batch individually
				for _, t := range batch {
					singleQuery := fmt.Sprintf(`count(%s%s)`, t.PromQLQueryName, matchersStr)
					f, sErr := executePromQLSingle(ctx, client, endpoint, token, projectID, singleQuery)
					mu.Lock()
					r := results[t.Name]
					r.Found = f
					if sErr != nil {
						r.Err = sErr.Error()
					}
					results[t.Name] = r
					mu.Unlock()
				}
				return
			}

			mu.Lock()
			for _, t := range batch {
				r := results[t.Name]
				r.Found = foundInBatch[t.Name]
				results[t.Name] = r
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

func executePromQL(ctx context.Context, client *http.Client, endpoint string, token string, projectID string, query string) (map[string]bool, error) {
	data := url.Values{}
	data.Set("query", query)

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
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var pResp promQLResponse
	if err := json.Unmarshal(body, &pResp); err != nil {
		return nil, err
	}
	if pResp.Status != "success" {
		return nil, fmt.Errorf("PromQL error: %s: %s", pResp.ErrorType, pResp.Error)
	}

	found := make(map[string]bool)
	for _, r := range pResp.Data.Result {
		if metricName, ok := r.Metric["metric"]; ok {
			found[metricName] = true
		}
	}
	return found, nil
}

func executePromQLSingle(ctx context.Context, client *http.Client, endpoint string, token string, projectID string, query string) (bool, error) {
	data := url.Values{}
	data.Set("query", query)

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return false, err
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
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var pResp promQLResponse
	if err := json.Unmarshal(body, &pResp); err != nil {
		return false, err
	}
	if pResp.Status != "success" {
		return false, fmt.Errorf("%s", pResp.Error)
	}

	return len(pResp.Data.Result) > 0, nil
}

// GCM response structures
type gcmTimeSeriesResponse struct {
	TimeSeries []struct {
		Metric struct {
			Type   string            `json:"type"`
			Labels map[string]string `json:"labels"`
		} `json:"metric"`
	} `json:"timeSeries"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func verifyGCM(ctx context.Context, client *http.Client, projectID string, token string, isStaging bool, targets []MetricTarget, namespace string, cluster string, lookback time.Duration, concurrency int) map[string]MetricResult {
	results := make(map[string]MetricResult, len(targets))
	for _, t := range targets {
		results[t.Name] = MetricResult{Target: t, Found: false}
	}

	if concurrency <= 0 {
		concurrency = 25
	}

	now := time.Now().UTC()
	startTime := now.Add(-lookback).Format(time.RFC3339)
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
			params.Set("view", "HEADERS")
			params.Set("pageSize", "1")

			reqURL := endpoint + "?" + params.Encode()
			req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
			if err != nil {
				mu.Lock()
				r := results[target.Name]
				r.Err = err.Error()
				results[target.Name] = r
				mu.Unlock()
				return
			}
			req.Header.Set("Authorization", "Bearer "+token)
			if projectID != "" {
				req.Header.Set("X-Goog-User-Project", projectID)
			}

			resp, err := client.Do(req)
			if err != nil {
				mu.Lock()
				r := results[target.Name]
				r.Err = err.Error()
				results[target.Name] = r
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			body, err := io.ReadAll(resp.Body)
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
			}
			results[target.Name] = r
			mu.Unlock()
		}()
	}

	wg.Wait()
	return results
}

func summarizeResults(mode string, results map[string]MetricResult, duration time.Duration) VerificationSummary {
	summary := VerificationSummary{
		Mode:          mode,
		TotalExpected: len(results),
		DurationMs:    duration.Milliseconds(),
		ByType:        make(map[MetricType]TypeStat),
	}

	for _, r := range results {
		stat := summary.ByType[r.Target.Type]
		stat.Expected++
		if r.Found {
			stat.Found++
			summary.TotalFound++
		} else {
			stat.Missing++
			summary.TotalMissing++
			summary.MissingMetrics = append(summary.MissingMetrics, r.Target.Name)
		}
		summary.ByType[r.Target.Type] = stat
	}

	sort.Strings(summary.MissingMetrics)
	summary.Passed = summary.TotalMissing == 0 && summary.TotalExpected > 0
	return summary
}

func printSummaryTable(w io.Writer, s VerificationSummary) {
	fmt.Fprintf(w, "\n--- Metric Verification Summary (Mode: %s, Duration: %.2fs) ---\n", s.Mode, float64(s.DurationMs)/1000.0)
	tw := tabwriter.NewWriter(w, 0, 0, 3, ' ', 0)
	fmt.Fprintln(tw, "METRIC TYPE\tEXPECTED\tQUERYABLE\tMISSING\tSTATUS")

	types := []MetricType{TypeGauge, TypeCounter, TypeHistogram, TypeNativeHistogram, TypeSummary}
	for _, t := range types {
		stat, ok := s.ByType[t]
		if !ok || stat.Expected == 0 {
			continue
		}
		status := "OK"
		if stat.Missing > 0 {
			status = "MISSING"
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%s\n", t, stat.Expected, stat.Found, stat.Missing, status)
	}

	fmt.Fprintln(tw, "--------------------------------------------------------")
	totalStatus := "PASSED"
	if !s.Passed {
		totalStatus = "FAILED"
	}
	fmt.Fprintf(tw, "TOTAL\t%d\t%d\t%d\t%s\n", s.TotalExpected, s.TotalFound, s.TotalMissing, totalStatus)
	_ = tw.Flush()

	if len(s.MissingMetrics) > 0 {
		fmt.Fprintf(w, "\nMissing / Unqueryable metrics (%d):\n", len(s.MissingMetrics))
		maxShow := 25
		for i, m := range s.MissingMetrics {
			if i >= maxShow {
				fmt.Fprintf(w, "  ... and %d more\n", len(s.MissingMetrics)-maxShow)
				break
			}
			fmt.Fprintf(w, "  - %s\n", m)
		}
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
	benchName := flag.String("bench-name", "", "Benchmark name (also Kubernetes namespace where avalanche runs).")
	namespace := flag.String("namespace", "", "Kubernetes namespace (defaults to bench-name).")
	clusterName := flag.String("cluster-name", "", "Cluster name to filter metrics.")
	projectID := flag.String("project-id", "", "Google Cloud project ID (defaults to gcloud current project).")
	mode := flag.String("mode", "promql", "Verification mode: 'promql', 'gcm', or 'both'.")
	staging := flag.Bool("staging", false, "Use staging Google Cloud Monitoring endpoint (staging-monitoring.sandbox.googleapis.com).")
	promqlURL := flag.String("promql-url", "", "PromQL query endpoint URL. Defaults to GCP Managed Prometheus endpoint.")
	token := flag.String("token", "", "Bearer token for API authorization. Defaults to ADC/gcloud auth token.")
	scenarioDir := flag.String("scenario", "", "Scenario directory path (e.g. ./manifests/scenarios/gmp).")
	loadManifest := flag.String("load-manifest", "", "Path to avalanche YAML manifest. Defaults to ./manifests/load/avalanche.exampletarget.yaml.")
	matchers := flag.String("matchers", "", "Extra PromQL label matchers in format 'k=v,k2=v2'.")
	format := flag.String("format", "table", "Output format: 'table' or 'json'.")

	concurrency := flag.Int("concurrency", 20, "Number of concurrent workers for querying.")
	batchSize := flag.Int("batch-size", 25, "Number of metrics to batch in each PromQL union query.")
	timeout := flag.Duration("timeout", 60*time.Second, "Overall verification timeout.")
	lookback := flag.Duration("lookback", 15*time.Minute, "Lookback interval for GCM time series query.")

	// Explicit avalanche configuration overrides
	gaugeCount := flag.Int("gauge-metric-count", -1, "Override gauge metric count.")
	counterCount := flag.Int("counter-metric-count", -1, "Override counter metric count.")
	histogramCount := flag.Int("histogram-metric-count", -1, "Override classic histogram metric count.")
	nativeHistogramCount := flag.Int("native-histogram-metric-count", -1, "Override native histogram metric count.")
	summaryCount := flag.Int("summary-metric-count", -1, "Override summary metric count.")
	metricLength := flag.Int("metricname-length", -1, "Override metricname length.")
	metricInterval := flag.Int("metric-interval", -1, "Override metric interval.")

	flag.Parse()

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

	// Attempt 1: From Kubernetes deployment if bench-name / namespace is given
	if ns != "" {
		args, err := GetDeploymentArgsFromK8s(ctx, ns)
		if err == nil && len(args) > 0 {
			avalancheArgs = args
			if *format != "json" {
				fmt.Printf("Loaded Avalanche configuration from Kubernetes deployment avalanche in namespace %s\n", ns)
			}
		}
	}

	// Attempt 2: From load manifest or scenario dir
	if len(avalancheArgs) == 0 {
		manifestPath := *loadManifest
		if manifestPath == "" && *scenarioDir != "" {
			cand := filepath.Join(*scenarioDir, "avalanche.yaml")
			if _, err := os.Stat(cand); err == nil {
				manifestPath = cand
			}
		}
		if manifestPath == "" {
			defaultPath := "./manifests/load/avalanche.exampletarget.yaml"
			if _, err := os.Stat(defaultPath); err == nil {
				manifestPath = defaultPath
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
	if *metricLength >= 0 {
		cfg.MetricNameLength = *metricLength
	}
	if *metricInterval >= 0 {
		cfg.MetricInterval = *metricInterval
	}

	targets := GenerateMetrics(cfg)
	if len(targets) == 0 {
		log.Fatal("No metrics generated! Check avalanche arguments or pass flags (e.g. -gauge-metric-count=100).")
	}

	if *format != "json" {
		fmt.Printf("Expected %d metric families from Avalanche (gauges: %d, counters: %d, histograms: %d, native-histograms: %d, summaries: %d)\n",
			len(targets), cfg.GaugeMetricCount, cfg.CounterMetricCount, cfg.HistogramMetricCount, cfg.NativeHistogramMetricCount, cfg.SummaryMetricCount)
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

	allPassed := true

	// 3. Execute PromQL verification
	if *mode == "promql" || *mode == "both" {
		if *format != "json" {
			stagingLabel := ""
			if isStaging {
				stagingLabel = " [staging]"
			}
			fmt.Printf("Verifying %d metrics via PromQL%s at %s with selector %s ...\n", len(targets), stagingLabel, endpointPromQL, matchersStr)
		}
		start := time.Now()
		promResults := verifyPromQL(ctx, httpClient, endpointPromQL, authToken, proj, targets, matchersStr, *batchSize, *concurrency)
		elapsed := time.Since(start)

		summary := summarizeResults("PromQL", promResults, elapsed)
		if *format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(summary)
		} else {
			printSummaryTable(os.Stdout, summary)
		}
		if !summary.Passed {
			allPassed = false
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
			fmt.Printf("Verifying %d metrics via GCM TimeSeries API%s (project: %s, namespace: %s, cluster: %s) ...\n", len(targets), stagingLabel, proj, ns, *clusterName)
		}
		start := time.Now()
		gcmResults := verifyGCM(ctx, httpClient, proj, authToken, isStaging, targets, ns, *clusterName, *lookback, *concurrency)
		elapsed := time.Since(start)

		summary := summarizeResults("GCM", gcmResults, elapsed)
		if *format == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(summary)
		} else {
			printSummaryTable(os.Stdout, summary)
		}
		if !summary.Passed {
			allPassed = false
		}
	}

	if !allPassed {
		os.Exit(1)
	}
}
