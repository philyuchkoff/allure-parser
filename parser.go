package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// Структуры данных Allure
type (
	AllureEnvironment map[string]string

	AllureSummary struct {
		Statistic struct {
			Passed  int `json:"passed"`
			Failed  int `json:"failed"`
			Broken  int `json:"broken"`
			Skipped int `json:"skipped"`
		} `json:"statistic"`
		Time struct {
			Duration int64 `json:"duration"`
		} `json:"time"`
	}

	AllureTestCase struct {
		UUID    string `json:"uuid"`
		Name    string `json:"name"`
		Status  string `json:"status"`
		Start   int64  `json:"start"`
		Stop    int64  `json:"stop"`
		Labels  []Label `json:"labels"`
		Steps   []Step  `json:"steps"`
	}

	Label struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	Step struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}

	AllureHistoryTrend struct {
		Items []HistoryItem `json:"items"`
	}

	HistoryItem struct {
		Data struct {
			Failed int `json:"failed"`
		} `json:"data"`
	}
)

// Глобальные переменные
var (
	logger *zap.Logger
	lastParseTime time.Time

	// Реестр метрик
	metrics = struct {
		testsTotal       *prometheus.GaugeVec
		suiteDuration    prometheus.Gauge
		testDuration     *prometheus.GaugeVec
		testStatus       *prometheus.GaugeVec
		flakyRatio       prometheus.Gauge
		environmentInfo  *prometheus.GaugeVec
		historyTrend     *prometheus.GaugeVec
		testsByLabel     *prometheus.GaugeVec
		stepsTotal       *prometheus.GaugeVec
	}{
		testsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_tests_total",
				Help: "Total tests by status",
			},
			[]string{"status"},
		),
		suiteDuration: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "allure_suite_duration_seconds",
				Help: "Test suite duration",
			},
		),
		testDuration: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_test_duration_seconds",
				Help: "Individual test duration",
			},
			[]string{"name", "suite"},
		),
		testStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_test_status",
				Help: "Test status (1-passed, 0-failed/broken)",
			},
			[]string{"name", "status", "severity"},
		),
		flakyRatio: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "allure_flaky_tests_ratio",
				Help: "Ratio of flaky tests",
			},
		),
		environmentInfo: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_environment_info",
				Help: "Test environment information",
			},
			[]string{"key", "value"},
		),
		historyTrend: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_history_failed_tests",
				Help: "Failed tests history trend",
			},
			[]string{"build"},
		),
		testsByLabel: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_tests_by_label",
				Help: "Tests grouped by label",
			},
			[]string{"label_type", "label_value"},
		),
		stepsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "allure_test_steps_total",
				Help: "Test steps by status",
			},
			[]string{"test_name", "status"},
		),
	}
)

func init() {
	// Инициализация логгера
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		fmt.Printf("Failed to init logger: %v\n", err)
		os.Exit(1)
	}

	// Регистрация метрик
	prometheus.MustRegister(metrics.testsTotal)
	prometheus.MustRegister(metrics.suiteDuration)
	prometheus.MustRegister(metrics.testDuration)
	prometheus.MustRegister(metrics.testStatus)
	prometheus.MustRegister(metrics.flakyRatio)
	prometheus.MustRegister(metrics.environmentInfo)
	prometheus.MustRegister(metrics.historyTrend)
	prometheus.MustRegister(metrics.testsByLabel)
	prometheus.MustRegister(metrics.stepsTotal)
}

func main() {
	defer logger.Sync()

	if len(os.Args) < 2 {
		logger.Fatal("Usage: ./allure-parser <path-to-allure-results> [<port>]")
	}

	port := "8080"
	if len(os.Args) > 2 {
		port = os.Args[2]
	}

	// Запуск парсера
	go runParser(os.Args[1])

	// HTTP сервер
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", healthCheck)

	logger.Info("Starting server", zap.String("port", port))
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Fatal("Server failed", zap.Error(err))
	}
}

func runParser(path string) {
	// Первоначальный парсинг
	if err := parseAllureReports(path); err != nil {
		logger.Error("Initial parse failed", zap.Error(err))
	}

	// Периодическое обновление
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := parseAllureReports(path); err != nil {
			logger.Error("Periodic parse failed", zap.Error(err))
		}
	}
}

func parseAllureReports(path string) error {
	startTime := time.Now()
	defer func() {
		lastParseTime = time.Now()
		logger.Info("Parsing completed", 
			zap.Duration("duration", time.Since(startTime)))
	}()

	// Сброс старых метрик
	resetMetrics()

	// 1. Парсинг environment
	if err := parseEnvironment(filepath.Join(path, "environment.json")); err != nil {
		logger.Warn("Environment parse failed", zap.Error(err))
	}

	// 2. Парсинг summary
	summary, err := parseSummary(filepath.Join(path, "widgets", "summary.json"))
	if err != nil {
		return fmt.Errorf("summary parse failed: %w", err)
	}
	updateSummaryMetrics(summary)

	// 3. Парсинг history trend
	if history, err := parseHistoryTrend(filepath.Join(path, "widgets", "history-trend.json")); err == nil {
		updateHistoryMetrics(history)
	} else {
		logger.Warn("History trend parse failed", zap.Error(err))
	}

	// 4. Парсинг тест-кейсов
	testFiles, err := filepath.Glob(filepath.Join(path, "data", "test-cases", "*.json"))
	if err != nil {
		return fmt.Errorf("test cases glob failed: %w", err)
	}

	for _, testFile := range testFiles {
		tc, err := parseTestCase(testFile)
		if err != nil {
			logger.Warn("Test case parse failed", 
				zap.String("file", testFile), 
				zap.Error(err))
			continue
		}
		updateTestCaseMetrics(tc)
	}

	return nil
}

func resetMetrics() {
	metrics.testsTotal.Reset()
	metrics.testDuration.Reset()
	metrics.testStatus.Reset()
	metrics.environmentInfo.Reset()
	metrics.historyTrend.Reset()
	metrics.testsByLabel.Reset()
	metrics.stepsTotal.Reset()
}

// Парсинг отдельных файлов
func parseEnvironment(path string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	var env AllureEnvironment
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}

	for k, v := range env {
		metrics.environmentInfo.WithLabelValues(k, v).Set(1)
	}

	return nil
}

func parseSummary(path string) (*AllureSummary, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var summary AllureSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return &summary, nil
}

func parseHistoryTrend(path string) (*AllureHistoryTrend, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var history AllureHistoryTrend
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return &history, nil
}

func parseTestCase(path string) (*AllureTestCase, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	var tc AllureTestCase
	if err := json.Unmarshal(data, &tc); err != nil {
		return nil, fmt.Errorf("json unmarshal: %w", err)
	}

	return &tc, nil
}

// Обновление метрик
func updateSummaryMetrics(summary *AllureSummary) {
	metrics.testsTotal.WithLabelValues("passed").Set(float64(summary.Statistic.Passed))
	metrics.testsTotal.WithLabelValues("failed").Set(float64(summary.Statistic.Failed))
	metrics.testsTotal.WithLabelValues("broken").Set(float64(summary.Statistic.Broken))
	metrics.testsTotal.WithLabelValues("skipped").Set(float64(summary.Statistic.Skipped))
	metrics.suiteDuration.Set(float64(summary.Time.Duration) / 1000)
}

func updateHistoryMetrics(history *AllureHistoryTrend) {
	if len(history.Items) == 0 {
		return
	}

	failedCount := 0
	for i, item := range history.Items {
		metrics.historyTrend.WithLabelValues(fmt.Sprintf("build_%d", i)).Set(float64(item.Data.Failed))
		if item.Data.Failed > 0 {
			failedCount++
		}
	}

	flakyRatio := float64(failedCount) / float64(len(history.Items))
	metrics.flakyRatio.Set(flakyRatio)
}

func updateTestCaseMetrics(tc *AllureTestCase) {
	// Длительность теста
	duration := float64(tc.Stop-tc.Start) / 1000
	metrics.testDuration.WithLabelValues(tc.Name, getLabelValue(tc.Labels, "suite")).Set(duration)

	// Статус теста
	statusValue := 0.0
	if tc.Status == "passed" {
		statusValue = 1.0
	}
	metrics.testStatus.WithLabelValues(
		tc.Name, 
		tc.Status, 
		getLabelValue(tc.Labels, "severity"),
	).Set(statusValue)

	// Шаги теста
	stepsByStatus := make(map[string]int)
	for _, step := range tc.Steps {
		stepsByStatus[step.Status]++
	}
	for status, count := range stepsByStatus {
		metrics.stepsTotal.WithLabelValues(tc.Name, status).Set(float64(count))
	}

	// Группировка по тегам
	for _, label := range tc.Labels {
		if isUsefulLabel(label.Name) {
			metrics.testsByLabel.WithLabelValues(label.Name, label.Value).Inc()
		}
	}
}

// Вспомогательные функции
func getLabelValue(labels []Label, name string) string {
	for _, label := range labels {
		if strings.EqualFold(label.Name, name) {
			return label.Value
		}
	}
	return "unknown"
}

func isUsefulLabel(name string) bool {
	usefulLabels := map[string]bool{
		"epic":      true,
		"feature":   true,
		"story":     true,
		"severity":  true,
		"owner":     true,
		"layer":     true,
	}
	return usefulLabels[strings.ToLower(name)]
}

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	if time.Since(lastParseTime) > 5*time.Minute {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("UNHEALTHY: Data is stale"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
