
# Парсер JSON-отчетов Allure, который экспортирует метрики в формате Prometheus

## Как использовать:
### Установите зависимости:

    go get github.com/prometheus/client_golang

### Запустите парсер:

    go run main.go path/to/allure-results

### Метрики будут доступны:

    http://localhost:8080/metrics

## Пример вывода метрик:

    # HELP allure_tests_total Total number of tests by status
    # TYPE allure_tests_total gauge
    allure_tests_total{status="passed"} 85
    allure_tests_total{status="failed"} 5
    allure_tests_total{status="broken"} 3
    allure_tests_total{status="skipped"} 7
    
    # HELP allure_suite_duration_seconds Total test suite duration in seconds
    # TYPE allure_suite_duration_seconds gauge
    allure_suite_duration_seconds 348.2
    
    # HELP allure_test_duration_seconds Individual test duration
    # TYPE allure_test_duration_seconds gauge
    allure_test_duration_seconds{test_name="login_test"} 12.5
