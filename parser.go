package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// AllureReport представляет структуру отчета Allure
type AllureReport struct {
	Summary    *AllureSummary
	TestCases  []*AllureTestCase
	Env        map[string]string
	History    *AllureHistory
}

// AllureSummary структура для summary.json
type AllureSummary struct {
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

// AllureTestCase структура тест-кейса
type AllureTestCase struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Start  int64  `json:"start"`
	Stop   int64  `json:"stop"`
	Labels []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"labels"`
}

// AllureHistory структура для history-trend.json
type AllureHistory struct {
	Items []struct {
		Data struct {
			Failed int `json:"failed"`
		} `json:"data"`
	} `json:"items"`
}

var (
	logger *zap.Logger
	report AllureReport

	// Метрики Prometheus
	testsTotal = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "allure_tests_total",
			Help: "Total number of tests by status",
		},
		[]string{"status"},
	)

	suiteDuration = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "allure_suite_duration_seconds",
			Help: "Total test suite duration in seconds",
		},
	)

	testDuration = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "allure_test_duration_seconds",
			Help: "Individual test duration",
		},
		[]string{"test_name"},
	)

	testStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "allure_test_status",
			Help: "Test status (1 - passed, 0 - failed/broken)",
		},
		[]string{"test_name", "status"},
	)

	flakyTests = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "allure_flaky_tests_ratio",
			Help: "Ratio of flaky tests",
		},
	)
)

func init() {
	// Инициализация логгера
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Регистрация метрик
	prometheus.MustRegister(testsTotal)
	prometheus.MustRegister(suiteDuration)
	prometheus.MustRegister(testDuration)
	prometheus.MustRegister(testStatus)
	prometheus.MustRegister(flakyTests)
}

func main() {
	defer logger.Sync()

	// Проверка аргументов
	if len(os.Args) < 2 {
		logger.Fatal("Usage: ./allure-parser <path-to-allure-results>")
	}
	allureResultsPath := os.Args[1]

	// Запуск парсера в фоне
	go runParser(allureResultsPath)

	// HTTP сервер для метрик
	http.Handle("/metrics", promhttp.Handler())
	http.HandleFunc("/health", healthCheck)

	logger.Info("Starting server on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		logger.Fatal("HTTP server failed", zap.Error(err))
	}
}

func runParser(path string) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := parseAllReports(path); err != nil {
			logger.Error("Failed to parse reports", zap.Error(err))
			continue
		}
		updateMetrics()
	}
}

func parseAllReports(path string) error {
	report = AllureReport{}

	// Парсинг summary.json
	if err := parseFile(filepath.Join(path, "widgets", "summary.json"), &report.Summary); err != nil {
		return fmt.Errorf("summary parsing failed: %w", err)
	}

	// Парсинг environment.json
	if err := parseFile(filepath.Join(path, "environment.json"), &report.Env); err != nil {
		logger.Warn("Failed to parse environment", zap.Error(err))
	}

	// Парсинг history-trend.json
	if err := parseFile(filepath.Join(path, "widgets", "history-trend.json"), &report.History); err != nil {
		logger.Warn("Failed to parse history", zap.Error(err))
	}

	// Парсинг тест-кейсов
	testFiles, err := filepath.Glob(filepath.Join(path, "data", "test-cases", "*.json"))
	if err != nil {
		return fmt.Errorf("test cases glob failed: %w", err)
	}

	report.TestCases = make([]*AllureTestCase, 0, len(testFiles))
	for _, testFile := range testFiles {
		var tc AllureTestCase
		if err := parseFile(testFile, &tc); err != nil {
			logger.Warn("Failed to parse test case", 
				zap.String("file", testFile), 
				zap.Error(err))
			continue
		}
		report.TestCases = append(report.TestCases, &tc)
	}

	logger.Info("Successfully parsed reports",
		zap.Int("test_cases", len(report.TestCases)),
		zap.Any("summary", report.Summary))

	return nil
}

func parseFile(path string, target interface{}) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return fmt.Errorf("file read error: %w", err)
	}

	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	return nil
}

func updateMetrics() {
	// Обновление summary-метрик
	if report.Summary != nil {
		testsTotal.WithLabelValues("passed").Set(float64(report.Summary.Statistic.Passed))
		testsTotal.WithLabelValues("failed").Set(float64(report.Summary.Statistic.Failed))
		testsTotal.WithLabelValues("broken").Set(float64(report.Summary.Statistic.Broken))
		testsTotal.WithLabelValues("skipped").Set(float64(report.Summary.Statistic.Skipped))

		suiteDuration.Set(float64(report.Summary.Time.Duration) / 1000)
	}

	// Обновление метрик тест-кейсов
	for _, tc := range report.TestCases {
		duration := float64(tc.Stop-tc.Start) / 1000
		testDuration.WithLabelValues(tc.Name).Set(duration)

		statusValue := 0.0
		if tc.Status == "passed" {
			statusValue = 1.0
		}
		testStatus.WithLabelValues(tc.Name, tc.Status).Set(statusValue)
	}

	// Расчет flaky-тестов
	if report.History != nil && len(report.History.Items) > 0 {
		failedRuns := 0
		for _, item := range report.History.Items {
			if item.Data.Failed > 0 {
				failedRuns++
			}
		}
		flakyRatio := float64(failedRuns) / float64(len(report.History.Items))
		flakyTests.Set(flakyRatio)
	}
}

func healthCheck(w http.ResponseWriter, _ *http.Request) {
	if report.Summary == nil || len(report.TestCases) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("UNHEALTHY: No valid report data"))
		return
	}

	if time.Since(report.Summary.Time.Duration) > 2*time.Hour {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("UNHEALTHY: Data is stale"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
