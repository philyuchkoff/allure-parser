#!/usr/bin/env python3
"""
Скрипт для получения результатов тестов из Allure TestOps и экспорта метрик в Prometheus.
Собирает данные о последнем запуске тестов, статистику и детальные результаты,
и предоставляет их в формате, пригодном для сбора Prometheus.
"""

import logging
import sys
import os
import time
from typing import Dict, List, Any, Optional

# Добавляем корневую директорию проекта в путь для импорта модуля core
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..'))

# Инициализация логирования ДО всех импортов
root = logging.getLogger()
root.setLevel(logging.INFO)
handler = logging.StreamHandler(sys.stdout)
formatter = logging.Formatter('%(asctime)s %(levelname)s %(message)s')
handler.setFormatter(formatter)
root.addHandler(handler)

# Импорт после настройки логирования и добавления пути
try:
    from core.client.allure_client import AllureClient
except ImportError as e:
    logging.error(f"Ошибка импорта AllureClient: {e}")
    logging.error("Убедитесь, что:")
    logging.error("1. Вы запускаете скрипт из корневой директории проекта")
    logging.error("2. Существует модуль core.client.allure_client")
    logging.error("3. В директориях есть файлы __init__.py")
    sys.exit(1)

# Импорт Prometheus клиента
try:
    from prometheus_client import start_http_server, Gauge, Counter, Histogram
except ImportError:
    logging.error("Требуется установить prometheus-client: pip install prometheus-client")
    sys.exit(1)

# Конфигурация (в продакшене вынести в переменные окружения или config файл)
ALLURE_ENDPOINT = 'https://allure.xxx.tech'
ALLURE_PROJECT_ID = '50'
ALLURE_TOKEN = os.getenv('ALLURE_TOKEN', '')  # Безопасное хранение токена в env (export ALLURE_TOKEN="token")
LAUNCH_NAME = 'openstack-tests'
PROMETHEUS_PORT = 8000  # Порт для метрик Prometheus, или другой, для исклбчения конфликтов

# Проверка наличия токена
if not ALLURE_TOKEN:
    logging.error("Переменная окружения ALLURE_TOKEN не установлена")
    sys.exit(1)

# Инициализация клиента Allure
allure_client = AllureClient(
    endpoint=ALLURE_ENDPOINT, 
    project_id=ALLURE_PROJECT_ID, 
    allure_token=ALLURE_TOKEN
)

# ===== PROMETHEUS METRICS DEFINITION =====
# Создание и описание метрик Prometheus

# Время старта запуска тестов (Unix timestamp)
LAUNCH_START_TIME = Gauge(
    'allure_launch_start_time', 
    'Start time of the launch (Unix timestamp)', 
    ['launch_name', 'launch_id']  # Лейблы для группировки
)

# Статус теста (числовое представление)
TEST_STATUS = Gauge(
    'allure_test_status', 
    'Status of a test (1=passed, 0=failed, -1=broken, -2=skipped, -99=unknown)', 
    ['launch_name', 'launch_id', 'test_name', 'test_fullname']
)

# Длительность выполнения теста в секундах
TEST_DURATION = Gauge(
    'allure_test_duration_seconds', 
    'Duration of a test in seconds', 
    ['launch_name', 'launch_id', 'test_name', 'test_fullname']
)

# Общее количество тестов по статусам
TESTS_TOTAL = Counter(
    'allure_tests_total', 
    'Total number of tests', 
    ['launch_name', 'launch_id', 'status']
)

# Гистограмма распределения времени выполнения тестов
TEST_DURATION_HISTOGRAM = Histogram(
    'allure_test_duration_seconds_histogram', 
    'Test duration distribution',
    ['launch_name', 'launch_id'],
    buckets=[0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 30.0, 60.0, 120.0]  # для статистики
)

# Время последнего успешного обновления метрик
LAST_UPDATE_TIME = Gauge(
    'allure_last_update_time', 
    'Last successful update time (Unix timestamp)'
)

# Счетчик ошибок при обновлении метрик
UPDATE_ERRORS = Counter(
    'allure_update_errors_total', 
    'Total number of update errors'
)

# Счетчик успешных обновлений метрик
UPDATE_SUCCESS = Counter(
    'allure_update_success_total',
    'Total number of successful updates'
)

# ===== ALLURE API FUNCTIONS =====

def get_last_launch_id(name: str) -> str:
#    Получает ID последнего запуска тестов по имени
    try:
        launches = allure_client.search_launch_by_name(name)
        if not launches:
            raise ValueError(f"Не найдено ни одного запуска с именем '{name}'")
        
        logging.info(f'Найдено запусков: {len(launches)}. Последний: {launches[0].get("id")}')
        return launches[0].get('id')
    
    except Exception as e:
        logging.error(f"Ошибка при получении ID запуска {name}: {e}")
        raise

def get_launch_statistics(launch_id: str) -> Dict[str, Any]:
#   Получает общую статистику по запуску.
    try:
        statistics = allure_client.get_statistic(launch_id=launch_id)
        logging.info(f'Статистика запуска {launch_id}: {statistics}')
        return statistics
    except Exception as e:
        logging.error(f"Ошибка при получении статистики запуска {launch_id}: {e}")
        raise

def get_test_results(launch_id: str) -> List[Dict[str, Any]]:
#   Получает детальные результаты тестов запуска из Allure TestOps API
    try:
        # Получаем ответ от API
        api_response = allure_client.test_results(launch_id=launch_id)
        
        # Проверяем, что ответ содержит ожидаемую структуру
        if not isinstance(api_response, dict):
            logging.error(f"Некорректный формат ответа API для запуска {launch_id}: {type(api_response)}")
            raise ValueError("Ответ API должен быть словарем")
        
        # Извлекаем контент из ответа
        results = api_response.get('content')
        
        # Проверяем, что content существует и является списком
        if results is None:
            logging.warning(f"Поле 'content' отсутствует в ответе для запуска {launch_id}")
            return []
        
        if not isinstance(results, list):
            logging.error(f"Поле 'content' должно быть списком, получено: {type(results)}")
            raise ValueError("Поле 'content' должно содержать список результатов")
        
        logging.info(f'Успешно получено результатов тестов: {len(results)} для запуска {launch_id}')
        
        # Дополнительная диагностика первого элемента
        if results and len(results) > 0:
            first_result = results[0]
            logging.debug(f"Пример структуры теста: keys={list(first_result.keys())}")
        
        return results
        
    except KeyError as e:
        logging.error(f"Отсутствует ожидаемое поле в ответе API для запуска {launch_id}: {e}")
        raise ValueError(f"Неполный ответ от API: {e}")
    except Exception as e:
        logging.error(f"Ошибка при получении результатов тестов {launch_id}: {e}")
        raise

# ===== PROMETHEUS METRICS FUNCTIONS =====

def update_prometheus_metrics(launch_id: str, launch_name: str, test_results: List[Dict[str, Any]]) -> None:
#   Обновляет все метрики Prometheus на основе данных из Allure
    status_count = {}  # Счетчик тестов по статусам
    
    for test in test_results:
        try:
            test_name = test.get('name', 'unknown')
            test_fullname = test.get('fullName', test_name)  # Уникальное полное имя
            status = test.get('status', 'unknown')
            duration_ms = test.get('duration', 0)
            
            # Конвертируем статус в числовое значение для метрики
            status_value = {
                'passed': 1,
                'failed': 0, 
                'broken': -1,
                'skipped': -2
            }.get(status.lower(), -99)  # -99 для неизвестных статусов
            
            # Устанавливаем значения метрик с лейблами
            TEST_STATUS.labels(
                launch_name=launch_name,
                launch_id=launch_id,
                test_name=test_name,
                test_fullname=test_fullname
            ).set(status_value)
            
            # Конвертируем миллисекунды в секунды (стандарт для Prometheus)
            duration_seconds = duration_ms / 1000.0
            TEST_DURATION.labels(
                launch_name=launch_name, 
                launch_id=launch_id,
                test_name=test_name,
                test_fullname=test_fullname
            ).set(duration_seconds)
            
            # Добавляем в гистограмму распределения
            TEST_DURATION_HISTOGRAM.labels(
                launch_name=launch_name,
                launch_id=launch_id
            ).observe(duration_seconds)
            
            # Считаем статистику по статусам
            status_count[status] = status_count.get(status, 0) + 1
            
        except Exception as e:
            logging.error(f"Ошибка обработки теста {test.get('name')}: {e}")
            continue
    
    # Обновляем счетчики тестов по статусам
    for status, count in status_count.items():
        TESTS_TOTAL.labels(
            launch_name=launch_name,
            launch_id=launch_id, 
            status=status
        ).inc(count)
    
    # Устанавливаем время старта запуска (используем текущее время как пример)
    LAUNCH_START_TIME.labels(
        launch_name=launch_name,
        launch_id=launch_id
    ).set(time.time())
    
    logging.info(f"Обновлены метрики для {len(test_results)} тестов")

def get_test_data(test_info: dict) -> str:
#   Форматирует информацию о тесте для логов
    test_name = test_info.get('name', 'unknown')
    duration = test_info.get('duration', 0)
    status = test_info.get('status', 'unknown')
    
    return f'Тест {test_name} был пройден за {duration} мс с результатом {status}'

# ===== MAIN EXECUTION =====

def main() -> None:
# Основная функция выполнения скрипта
    # Запускаем http-сервер для Prometheus
    start_http_server(PROMETHEUS_PORT)
    logging.info(f"Prometheus metrics server started on port {PROMETHEUS_PORT}")
    logging.info(f"Metrics available at http://localhost:{PROMETHEUS_PORT}/metrics")
    
    # Основной цикл обновления метрик
    update_interval = 300  # раз в пять минут, на проде надо поменять на реальное!!
    consecutive_errors = 0
    max_consecutive_errors = 3 # максимальное количество ошибок подряд при получении результатов
    
    while True:
        try:
            logging.info(f"Поиск последнего запуска: {LAUNCH_NAME}")
            launch_id = get_last_launch_id(name=LAUNCH_NAME)
            
            logging.info(f"Получение статистики запуска: {launch_id}")
            statistics = get_launch_statistics(launch_id=launch_id)
            
            logging.info(f"Получение результатов тестов: {launch_id}")
            results = get_test_results(launch_id=launch_id)
            
            # Обновляем метрики Prometheus
            logging.info(f"Обновление метрик Prometheus для запуска: {launch_id}")
            update_prometheus_metrics(launch_id, LAUNCH_NAME, results)
            
            # Обновляем время последнего успешного обновления
            LAST_UPDATE_TIME.set(time.time())
            UPDATE_SUCCESS.inc()
            consecutive_errors = 0  # Сбрасываем счетчик ошибок
            
            logging.info(f"Метрики успешно обновлены. Тестов: {len(results)}")
            
            # Логируем информацию о каждом тесте (опционально, для дебага, можно выкинуть потом)
            if results and len(results) > 0:
                for test in results[:3]:  # Логируем только первые 3 для примера
                    test_data = get_test_data(test_info=test)
                    logging.debug(test_data)
            
        except Exception as e:
            consecutive_errors += 1
            UPDATE_ERRORS.inc()
            logging.error(f"Ошибка при обновлении метрик: {e}")
            
            if consecutive_errors >= max_consecutive_errors:
                logging.error(f"Достигнуто максимальное количество ошибок подряд ({max_consecutive_errors}). Пауза перед повторной попыткой.")
                time.sleep(update_interval * 2)  # Удвоенная пауза при множественных ошибках
                consecutive_errors = 0
            else:
                logging.info(f"Повторная попытка через {update_interval} секунд...")
        
        # Пауза между обновлениями
        time.sleep(update_interval)

if __name__ == '__main__':
    try:
        main()
    except KeyboardInterrupt:
        logging.info("Скрипт остановлен пользователем")
    except Exception as e:
        logging.error(f"Необработанная ошибка: {e}")
        sys.exit(1)
