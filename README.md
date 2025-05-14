# Парсер JSON-отчетов Allure, который экспортирует метрики в формате Prometheus

## Как использовать:
### Установите зависимости:

    go get github.com/prometheus/client_golang

### Запустите парсер:

    go run main.go path/to/allure-results

### Метрики будут доступны:

    http://localhost:8080/metrics
