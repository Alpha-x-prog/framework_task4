package main

import (
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func main() {
	store := NewStore()
	metrics := NewMetrics()
	h := NewHandler(store, metrics)

	r := gin.Default()

	r.POST("/event", h.HandleEvent)
	r.GET("/process/:key", h.HandleGetProcess)
	r.GET("/health/live", h.HandleLive)
	r.GET("/health/ready", h.HandleReady)
	r.GET("/metrics", h.HandleMetrics)

	logger.Info("сервер запущен", "addr", ":8080")
	if err := r.Run(":8080"); err != nil {
		logger.Error("ошибка запуска", "error", err)
		os.Exit(1)
	}
}
