package main

import (
	"sync"
	"time"
)

// Metrics хранит счётчики и задержки шагов.
// Все методы потокобезопасны.
type Metrics struct {
	mu sync.Mutex

	TransitionsSuccess  int
	TransitionsError    int
	DuplicateDeliveries int
	Compensations       int

	latencies map[Event][]time.Duration
}

func NewMetrics() *Metrics {
	return &Metrics{latencies: make(map[Event][]time.Duration)}
}

func (m *Metrics) IncSuccess()     { m.mu.Lock(); m.TransitionsSuccess++; m.mu.Unlock() }
func (m *Metrics) IncError()       { m.mu.Lock(); m.TransitionsError++; m.mu.Unlock() }
func (m *Metrics) IncDuplicate()   { m.mu.Lock(); m.DuplicateDeliveries++; m.mu.Unlock() }
func (m *Metrics) IncCompensation() { m.mu.Lock(); m.Compensations++; m.mu.Unlock() }

func (m *Metrics) RecordLatency(event Event, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies[event] = append(m.latencies[event], d)
}

// ErrorCount возвращает количество ошибок переходов (для readiness probe).
func (m *Metrics) ErrorCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.TransitionsError
}

// Snapshot возвращает снимок всех метрик для эндпоинта /metrics.
func (m *Metrics) Snapshot() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	latencies := make(map[string]float64, len(m.latencies))
	for event, durations := range m.latencies {
		if len(durations) == 0 {
			continue
		}
		var total time.Duration
		for _, d := range durations {
			total += d
		}
		latencies[string(event)] = float64(total.Milliseconds()) / float64(len(durations))
	}

	return map[string]any{
		"transitions_success_total":  m.TransitionsSuccess,
		"transitions_error_total":    m.TransitionsError,
		"duplicate_deliveries_total": m.DuplicateDeliveries,
		"compensations_total":        m.Compensations,
		"avg_latency_ms_per_event":   latencies,
	}
}
