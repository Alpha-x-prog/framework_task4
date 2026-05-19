package main

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	store   *Store
	metrics *Metrics
}

func NewHandler(store *Store, metrics *Metrics) *Handler {
	return &Handler{store: store, metrics: metrics}
}

// ─── POST /event ─────────────────────────────────────────────────────────────

type EventRequest struct {
	ProcessKey      string `json:"process_key"     binding:"required"`
	Event           Event  `json:"event"           binding:"required"`
	IdempotencyKey  string `json:"idempotency_key" binding:"required"`
	CorrelationID   string `json:"correlation_id"`
	SimulateFailure bool   `json:"simulate_failure"`
}

func (h *Handler) HandleEvent(c *gin.Context) {
	var req EventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.CorrelationID == "" {
		req.CorrelationID = fmt.Sprintf("auto-%d", time.Now().UnixNano())
	}
	c.Header("X-Correlation-ID", req.CorrelationID)

	h.store.Lock()
	defer h.store.Unlock()

	// Проверка идемпотентности
	if savedState, seen := h.store.CheckIdempotency(req.IdempotencyKey); seen {
		h.metrics.IncDuplicate()
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

	proc := h.store.GetOrCreate(req.ProcessKey)
	fromState := proc.State
	start := time.Now()

	logType, err := Transition(proc, req.Event, req.SimulateFailure)
	elapsed := time.Since(start)
	h.metrics.RecordLatency(req.Event, elapsed)

	// Сохраняем ключ идемпотентности в любом случае
	h.store.SaveIdempotency(req.IdempotencyKey, string(proc.State))

	if err != nil {
		h.metrics.IncError()
		logger.Error("переход отклонён",
			"type", "transition_error",
			"event", req.Event,
			"from_state", fromState,
			"process_key", req.ProcessKey,
			"correlation_id", req.CorrelationID,
			"error", err.Error(),
			"latency_ms", elapsed.Milliseconds(),
		)
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error":          err.Error(),
			"state":          proc.State,
			"correlation_id": req.CorrelationID,
		})
		return
	}

	if logType == "compensation" {
		h.metrics.IncCompensation()
	}
	h.metrics.IncSuccess()

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

// ─── GET /process/:key ────────────────────────────────────────────────────────

func (h *Handler) HandleGetProcess(c *gin.Context) {
	h.store.Lock()
	proc, ok := h.store.Get(c.Param("key"))
	h.store.Unlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "процесс не найден"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"process_key": proc.Key, "state": proc.State})
}

// ─── GET /health/live ─────────────────────────────────────────────────────────

func (h *Handler) HandleLive(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// ─── GET /health/ready ────────────────────────────────────────────────────────

func (h *Handler) HandleReady(c *gin.Context) {
	errCount := h.metrics.ErrorCount()
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

// ─── GET /metrics ─────────────────────────────────────────────────────────────

func (h *Handler) HandleMetrics(c *gin.Context) {
	c.JSON(http.StatusOK, h.metrics.Snapshot())
}
