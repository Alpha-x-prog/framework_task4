package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// ─── Машина состояний ────────────────────────────────────────────────────────

type State string

const (
	StateNew              State = "Новый"
	StateAppAccepted      State = "ЗаявкаПринята"
	StateResourceBooked   State = "РесурсЗабронирован"
	StateAccessGranted    State = "ДоступВыдан"
	StateCompleted        State = "Завершён"
	StateError            State = "Ошибка"
	StateCompensationDone State = "КомпенсацияВыполнена"
)

type Event string

const (
	EventAcceptApplication Event = "AcceptApplication"
	EventBook              Event = "Book"
	EventGrantAccess       Event = "GrantAccess"
	EventComplete          Event = "Complete"
)

// ─── Хранилище ───────────────────────────────────────────────────────────────

type Process struct {
	Key   string
	State State
}

var (
	mu              sync.Mutex
	processes       = map[string]*Process{}
	idempotencyKeys = map[string]string{} // idempotency_key → состояние после обработки
)

// ─── Метрики ─────────────────────────────────────────────────────────────────

var (
	metricsMu           sync.Mutex
	transitionsSuccess  int
	transitionsError    int
	duplicateDeliveries int
	compensations       int
	stepLatencies       = map[Event][]time.Duration{}
)

func recordLatency(event Event, d time.Duration) {
	metricsMu.Lock()
	defer metricsMu.Unlock()
	stepLatencies[event] = append(stepLatencies[event], d)
}

func avgLatencyMs(event Event) float64 {
	durations := stepLatencies[event]
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return float64(total.Milliseconds()) / float64(len(durations))
}

// ─── Логгер ──────────────────────────────────────────────────────────────────

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

// ─── Обработчик события ──────────────────────────────────────────────────────

type EventRequest struct {
	ProcessKey      string `json:"process_key" binding:"required"`
	Event           Event  `json:"event" binding:"required"`
	IdempotencyKey  string `json:"idempotency_key" binding:"required"`
	CorrelationID   string `json:"correlation_id"`
	SimulateFailure bool   `json:"simulate_failure"` // для тестирования сбоев
}

func handleEvent(c *gin.Context) {
	var req EventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Если correlation_id не передан — генерируем
	if req.CorrelationID == "" {
		req.CorrelationID = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	c.Header("X-Correlation-ID", req.CorrelationID)

	mu.Lock()
	defer mu.Unlock()

	// ── Проверка идемпотентности ──────────────────────────────────────────
	if savedState, seen := idempotencyKeys[req.IdempotencyKey]; seen {
		metricsMu.Lock()
		duplicateDeliveries++
		metricsMu.Unlock()

		logger.Info("повторная доставка проигнорирована",
			"type", "duplicate_delivery",
			"idempotency_key", req.IdempotencyKey,
			"process_key", req.ProcessKey,
			"current_state", savedState,
			"correlation_id", req.CorrelationID,
		)
		c.JSON(http.StatusOK, gin.H{
			"status":         "duplicate",
			"state":          savedState,
			"correlation_id": req.CorrelationID,
		})
		return
	}

	// ── Получить или создать процесс ──────────────────────────────────────
	proc, ok := processes[req.ProcessKey]
	if !ok {
		proc = &Process{Key: req.ProcessKey, State: StateNew}
		processes[req.ProcessKey] = proc
	}

	start := time.Now()
	fromState := proc.State

	// ── Переход машины состояний ──────────────────────────────────────────
	var transitionErr error
	logType := "transition"

	switch req.Event {

	case EventAcceptApplication:
		if proc.State != StateNew {
			transitionErr = fmt.Errorf("нельзя выполнить %s из состояния %s", req.Event, proc.State)
		} else if req.SimulateFailure {
			proc.State = StateError
		} else {
			proc.State = StateAppAccepted
		}

	case EventBook:
		if proc.State != StateAppAccepted {
			transitionErr = fmt.Errorf("нельзя выполнить %s из состояния %s", req.Event, proc.State)
		} else if req.SimulateFailure {
			proc.State = StateError
		} else {
			proc.State = StateResourceBooked
		}

	case EventGrantAccess:
		if proc.State != StateResourceBooked {
			transitionErr = fmt.Errorf("нельзя выполнить %s из состояния %s", req.Event, proc.State)
		} else if req.SimulateFailure {
			// Компенсация: отменяем бронирование (откат шага Book)
			metricsMu.Lock()
			compensations++
			metricsMu.Unlock()
			proc.State = StateCompensationDone
			logType = "compensation"

			logger.Info("компенсация: бронирование отменено",
				"type", "compensation",
				"event", req.Event,
				"process_key", req.ProcessKey,
				"correlation_id", req.CorrelationID,
			)
		} else {
			proc.State = StateAccessGranted
		}

	case EventComplete:
		if proc.State != StateAccessGranted {
			transitionErr = fmt.Errorf("нельзя выполнить %s из состояния %s", req.Event, proc.State)
		} else if req.SimulateFailure {
			proc.State = StateError
		} else {
			proc.State = StateCompleted
		}

	default:
		transitionErr = fmt.Errorf("неизвестное событие: %s", req.Event)
	}

	elapsed := time.Since(start)
	recordLatency(req.Event, elapsed)

	// Сохраняем ключ идемпотентности в любом случае
	idempotencyKeys[req.IdempotencyKey] = string(proc.State)

	if transitionErr != nil {
		metricsMu.Lock()
		transitionsError++
		metricsMu.Unlock()

		logger.Error("переход отклонён",
			"type", "transition_error",
			"event", req.Event,
			"from_state", fromState,
			"process_key", req.ProcessKey,
			"correlation_id", req.CorrelationID,
			"error", transitionErr.Error(),
			"latency_ms", elapsed.Milliseconds(),
		)
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":          transitionErr.Error(),
			"state":          proc.State,
			"correlation_id": req.CorrelationID,
		})
		return
	}

	metricsMu.Lock()
	transitionsSuccess++
	metricsMu.Unlock()

	logger.Info("переход выполнен",
		"type", logType,
		"event", req.Event,
		"from_state", fromState,
		"to_state", proc.State,
		"process_key", req.ProcessKey,
		"correlation_id", req.CorrelationID,
		"latency_ms", elapsed.Milliseconds(),
	)

	c.JSON(http.StatusOK, gin.H{
		"state":          proc.State,
		"correlation_id": req.CorrelationID,
	})
}

// ─── Состояние процесса ───────────────────────────────────────────────────────

func handleGetProcess(c *gin.Context) {
	key := c.Param("key")
	mu.Lock()
	proc, ok := processes[key]
	mu.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "процесс не найден"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"process_key": proc.Key, "state": proc.State})
}

// ─── Health checks ────────────────────────────────────────────────────────────

func handleLive(c *gin.Context) {
	// Живость: всегда OK пока процесс работает
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleReady(c *gin.Context) {
	// Готовность: неуспешна при критической деградации (более 10 ошибок)
	metricsMu.Lock()
	errCount := transitionsError
	metricsMu.Unlock()

	if errCount > 10 {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":      "degraded",
			"error_count": errCount,
			"reason":      "слишком много ошибок переходов",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}

// ─── Метрики ──────────────────────────────────────────────────────────────────

func handleMetrics(c *gin.Context) {
	metricsMu.Lock()
	defer metricsMu.Unlock()

	latencies := map[string]float64{}
	for event := range stepLatencies {
		latencies[string(event)] = avgLatencyMs(event)
	}

	c.JSON(http.StatusOK, gin.H{
		"transitions_success_total":  transitionsSuccess,
		"transitions_error_total":    transitionsError,
		"duplicate_deliveries_total": duplicateDeliveries,
		"compensations_total":        compensations,
		"avg_latency_ms_per_event":   latencies,
	})
}

// ─── main ─────────────────────────────────────────────────────────────────────

func main() {
	r := gin.Default()

	r.POST("/event", handleEvent)
	r.GET("/process/:key", handleGetProcess)
	r.GET("/health/live", handleLive)
	r.GET("/health/ready", handleReady)
	r.GET("/metrics", handleMetrics)

	fmt.Println("Сервер запущен на :8080")
	if err := r.Run(":8080"); err != nil {
		logger.Error("ошибка запуска сервера", "error", err)
		os.Exit(1)
	}
}
