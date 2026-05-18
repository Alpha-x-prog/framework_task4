# Task 4 — Booking State Machine Web Service

Учебный веб-сервис на Go + Gin, который моделирует процесс бронирования переговорки.  
Реализует машину состояний, идемпотентность, компенсацию и наблюдаемость.

## Машина состояний

```
Новый
  └─[AcceptApplication]─► ЗаявкаПринята
                              └─[Book]─► РесурсЗабронирован
                                            └─[GrantAccess]─► ДоступВыдан
                                            │                    └─[Complete]─► Завершён
                                            └─[GrantAccess + сбой]─► КомпенсацияВыполнена
                              └─[любой шаг + сбой]─► Ошибка
```

## Запуск

```bash
go run .
# сервер поднимается на :8080
```

## API

| Метод | Путь | Описание |
|-------|------|----------|
| `POST` | `/event` | Отправить событие процессу |
| `GET` | `/process/:key` | Текущее состояние процесса |
| `GET` | `/health/live` | Проверка живости |
| `GET` | `/health/ready` | Проверка готовности (503 при деградации) |
| `GET` | `/metrics` | Счётчики и задержки |

### Тело запроса POST /event

```json
{
  "process_key":      "booking-123",
  "event":            "AcceptApplication",
  "idempotency_key":  "unique-event-id",
  "correlation_id":   "req-trace-id",
  "simulate_failure": false
}
```

**События:** `AcceptApplication` → `Book` → `GrantAccess` → `Complete`

**`simulate_failure: true`** — принудительно вызывает сбой шага (для тестирования).

## Примеры

### Нормальный путь

```bash
curl -X POST http://localhost:8080/event \
  -H "Content-Type: application/json" \
  -d '{"process_key":"b1","event":"AcceptApplication","idempotency_key":"e1","correlation_id":"c1"}'
# {"state":"ЗаявкаПринята","correlation_id":"c1"}

curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b1","event":"Book","idempotency_key":"e2","correlation_id":"c1"}'
# {"state":"РесурсЗабронирован","correlation_id":"c1"}

curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b1","event":"GrantAccess","idempotency_key":"e3","correlation_id":"c1"}'
# {"state":"ДоступВыдан","correlation_id":"c1"}

curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b1","event":"Complete","idempotency_key":"e4","correlation_id":"c1"}'
# {"state":"Завершён","correlation_id":"c1"}
```

### Повторная доставка (идемпотентность)

```bash
# Повторная отправка того же idempotency_key не меняет состояние
curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b1","event":"AcceptApplication","idempotency_key":"e1","correlation_id":"c1"}'
# {"status":"duplicate","state":"ЗаявкаПринята","correlation_id":"c1"}
```

### Сбой GrantAccess + компенсация

```bash
curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b2","event":"AcceptApplication","idempotency_key":"f1"}'
curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b2","event":"Book","idempotency_key":"f2"}'
curl -X POST http://localhost:8080/event \
  -d '{"process_key":"b2","event":"GrantAccess","idempotency_key":"f3","simulate_failure":true}'
# {"state":"КомпенсацияВыполнена",...}
# Бронирование отменено, в логах запись type=compensation
```

## Наблюдаемость

### Логи (JSON, stdout)

Каждая запись содержит `correlation_id`. Типы записей:

| `type` | Когда |
|--------|-------|
| `transition` | Успешный переход состояния |
| `duplicate_delivery` | Повторная доставка события |
| `compensation` | Компенсация при сбое GrantAccess |
| `transition_error` | Недопустимый переход |

Пример:
```json
{"time":"2026-05-18T12:00:00Z","level":"INFO","msg":"переход выполнен",
 "type":"transition","event":"Book","from_state":"ЗаявкаПринята",
 "to_state":"РесурсЗабронирован","process_key":"b1","correlation_id":"c1","latency_ms":0}
```

### Метрики GET /metrics

```json
{
  "transitions_success_total":  10,
  "transitions_error_total":    2,
  "duplicate_deliveries_total": 1,
  "compensations_total":        1,
  "avg_latency_ms_per_event": {
    "AcceptApplication": 0.1,
    "Book":              0.0,
    "GrantAccess":       0.2,
    "Complete":          0.0
  }
}
```

### Health checks

```bash
GET /health/live   # 200 всегда, пока процесс жив
GET /health/ready  # 200 в норме / 503 при >10 ошибок переходов
```

## Сценарии проверки

Bash-скрипт с пятью сценариями (требует curl и запущенного сервера):

```bash
bash scenarios.sh
```

Сценарии:
1. Нормальный путь — все 4 шага успешно
2. Повторная доставка — дубль игнорируется
3. Сбой `GrantAccess` — компенсация
4. Сбой на первом шаге — состояние `Ошибка`
5. Health checks + метрики
