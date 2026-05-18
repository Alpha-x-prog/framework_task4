#!/bin/bash
# Сценарии проверки сервиса (запускать при работающем сервере на :8080)

BASE="http://localhost:8080"

echo "============================================"
echo "Сценарий 1: Нормальный путь (все 4 шага)"
echo "============================================"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-001","event":"AcceptApplication","idempotency_key":"evt-001-1","correlation_id":"corr-001"}' | python3 -m json.tool
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-001","event":"Book","idempotency_key":"evt-001-2","correlation_id":"corr-001"}' | python3 -m json.tool
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-001","event":"GrantAccess","idempotency_key":"evt-001-3","correlation_id":"corr-001"}' | python3 -m json.tool
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-001","event":"Complete","idempotency_key":"evt-001-4","correlation_id":"corr-001"}' | python3 -m json.tool

echo "Итоговое состояние (ожидаем: Завершён):"
curl -s $BASE/process/booking-001 | python3 -m json.tool


echo ""
echo "============================================"
echo "Сценарий 2: Повторная доставка (идемпотентность)"
echo "============================================"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-002","event":"AcceptApplication","idempotency_key":"evt-002-1","correlation_id":"corr-002"}' | python3 -m json.tool

echo "Тот же idempotency_key повторно — состояние не должно измениться:"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-002","event":"AcceptApplication","idempotency_key":"evt-002-1","correlation_id":"corr-002"}' | python3 -m json.tool


echo ""
echo "============================================"
echo "Сценарий 3: Сбой GrantAccess → компенсация"
echo "============================================"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-003","event":"AcceptApplication","idempotency_key":"evt-003-1","correlation_id":"corr-003"}' | python3 -m json.tool
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-003","event":"Book","idempotency_key":"evt-003-2","correlation_id":"corr-003"}' | python3 -m json.tool
echo "Сбой на GrantAccess — должна выполниться компенсация (отмена брони):"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-003","event":"GrantAccess","idempotency_key":"evt-003-3","correlation_id":"corr-003","simulate_failure":true}' | python3 -m json.tool
echo "Итог (ожидаем: КомпенсацияВыполнена):"
curl -s $BASE/process/booking-003 | python3 -m json.tool


echo ""
echo "============================================"
echo "Сценарий 4: Сбой на первом шаге (без компенсации)"
echo "============================================"
curl -s -X POST $BASE/event -H "Content-Type: application/json" -d '{"process_key":"booking-004","event":"AcceptApplication","idempotency_key":"evt-004-1","correlation_id":"corr-004","simulate_failure":true}' | python3 -m json.tool
echo "Итог (ожидаем: Ошибка):"
curl -s $BASE/process/booking-004 | python3 -m json.tool


echo ""
echo "============================================"
echo "Сценарий 5: Health checks"
echo "============================================"
echo "Liveness:"
curl -s $BASE/health/live | python3 -m json.tool
echo "Readiness:"
curl -s $BASE/health/ready | python3 -m json.tool


echo ""
echo "============================================"
echo "Метрики"
echo "============================================"
curl -s $BASE/metrics | python3 -m json.tool
