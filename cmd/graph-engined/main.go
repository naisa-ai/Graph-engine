// Package main is the entrypoint for the graph-engine service.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/naisa-ai/graph-engine/internal/api/grpc"
	"github.com/naisa-ai/graph-engine/internal/config"
	"github.com/naisa-ai/graph-engine/internal/service"
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", "config.yaml", "path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load configuration", "error", err, "path", *configPath)
		os.Exit(1)
	}

	// Initialize logger
	logger := initLogger(cfg)
	logger.Info("starting graph-engine",
		"config_path", *configPath,
		"grpc_port", cfg.Server.GRPCPort,
	)

	// Initialize build store
	buildStore := service.NewBuildStore(cfg.Limits.MaxBuildsPending)

	// Create and start gRPC server
	server, err := grpc.NewServer(cfg, logger, buildStore)
	if err != nil {
		logger.Error("failed to create gRPC server", "error", err)
		os.Exit(1)
	}

	// Start HTTP server for metrics
	httpServer := startMetricsServer(cfg, logger)

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := server.Serve(); err != nil {
			errChan <- err
		}
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigChan:
		logger.Info("received shutdown signal", "signal", sig.String())
		server.GracefulStop()
		shutdownMetricsServer(httpServer, logger)
	case err := <-errChan:
		logger.Error("server error", "error", err)
		os.Exit(1)
	}

	logger.Info("graph-engine stopped")
}

// initLogger creates a logger based on configuration.
func initLogger(cfg *config.Config) *slog.Logger {
	var handler slog.Handler

	// Determine log level
	var level slog.Level
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	// Choose handler based on format
	if cfg.Log.Format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// startMetricsServer starts the HTTP server for Prometheus metrics.
func startMetricsServer(cfg *config.Config, logger *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("OK")); err != nil {
			logger.Error("health check write failed", "error", err)
		}
	})

	server := &http.Server{
		Addr:    cfg.HTTPAddr(),
		Handler: mux,
	}

	go func() {
		logger.Info("starting metrics HTTP server", "addr", cfg.HTTPAddr())
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics HTTP server error", "error", err)
		}
	}()

	return server
}

// shutdownMetricsServer gracefully shuts down the HTTP server.
func shutdownMetricsServer(server *http.Server, logger *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Info("shutting down metrics HTTP server")
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("metrics HTTP server shutdown error", "error", err)
	}
}
