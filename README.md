

# Парсер JSON-отчетов Allure, который экспортирует метрики в формате Prometheus

## Как использовать:
### Установите зависимости:

    go get github.com/prometheus/client_golang
    go get go.uber.org/zap

### Соберите и запустите парсер:

    go build -o allure-parser .
    ./allure-parser path/to/allure-results

### Проверьте метрики:

    curl http://localhost:8080/metrics

### Проверьте "здоровье":

    curl http://localhost:8080/health

### Метрики будут доступны:

    http://localhost:8080/metrics

Метрики обновляются раз в 30 секунд.

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

## Пример логов:

    {"level":"info","ts":1630000000,"msg":"Successfully parsed reports","test_cases":42,"summary":{"statistic":{"passed":38,"failed":2,"broken":1,"skipped":1},"time":{"duration":120000}}}
    {"level":"warn","ts":1630000001,"msg":"Failed to parse environment","error":"file not found"}

## Что умеет
### Комплексное логирование:

 - использован zap для структурированного логирования 
 - разные уровни логов (Info, Warn, Error)
 - контекстные логи с полями

### Обработка ошибок:

 - проверка ошибок на всех этапах 
 - обертывание ошибок с контекстом (%w)
 - грейсфул деградация (пропуск битых файлов)

### Health Check:

 - эндпоинт `/health` для проверки состояния 
 - проверка актуальности данных

### Дополнительные метрики:

 - метрика flaky-тестов 
 - валидация данных перед экспортом

### Безопасность:

 - проверка аргументов командной строки
 - защита от паники
