

# Парсер JSON-отчетов Allure, который экспортирует метрики в формате Prometheus

## Как использовать:
### Установите зависимости:

    go get github.com/prometheus/client_golang
    go get go.uber.org/zap

### Соберите и запустите парсер:

    go build -o allure-parser .
    ./allure-parser path/to/allure-results

### Проверьте метрики:

    curl http://localhost:8080/metrics | grep allure_

### Проверьте "здоровье":

    curl http://localhost:8080/health

### Метрики будут доступны:

    http://localhost:8080/metrics

Метрики обновляются раз в 30 секунд.

## Пример вывода метрик:

    # Environment
    allure_environment_info{key="browser",value="chrome"} 1
    allure_environment_info{key="os",value="linux"} 1
    
    # History trends
    allure_history_failed_tests{build="build_0"} 2
    allure_history_failed_tests{build="build_1"} 1
    allure_flaky_tests_ratio 0.33
    
    # Grouping by tags
    allure_tests_by_label{label_type="epic",label_value="authentication"} 5
    allure_tests_by_label{label_type="severity",label_value="critical"} 3
    
    # Test steps
    allure_test_steps_total{test_name="login_test",status="passed"} 8

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
 - graceful degradation (пропуск битых файлов) и при частичных ошибках
 - подробное логирование проблем

### Health Check:

 - эндпоинт `/health` для проверки состояния 
 - проверка актуальности данных

### Дополнительные метрики:

 - метрика flaky-тестов 
 - валидация данных перед экспортом
-   количество шагов в тестах (`allure_test_steps_total`)
-   информация о severity (`allure_test_status`)

### Environment-метрики:
    
-   добавлен сбор данных из  `environment.json`
-   метрика  `allure_environment_info{key="os", value="linux"}`

### Исторические тренды:
    
-   парсинг  `history-trend.json`
-   метрики  `allure_history_failed_tests{build="build_N"}`
-   автоматический расчет  `allure_flaky_tests_ratio`

### Группировка по тегам:
    
-   поддержка популярных тегов (epic, feature, story)
-   метрика  `allure_tests_by_label{label_type="epic", label_value="auth"}`

### Безопасность:

 - проверка аргументов командной строки
 - защита от паники
