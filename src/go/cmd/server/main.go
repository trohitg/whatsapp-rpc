package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"whatsapp-rpc/src/go/config"
	server "whatsapp-rpc/src/go/rpc"
	"whatsapp-rpc/src/go/whatsapp"
)

func main() {
	// Initialize logger
	logger := logrus.New()
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	// Set log level (5 = Debug, 4 = Info)
	if cfg.LogLevel >= 5 {
		logger.SetLevel(logrus.DebugLevel)
	}

	logger.Info("Starting WhatsApp WebSocket RPC Server")

	// Initialize WhatsApp service
	whatsappService, err := whatsapp.NewService(cfg.Database, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize WhatsApp service: %v", err)
	}

	// Auto-start if there's an existing session (no need to press Start button)
	if whatsappService.HasExistingSession() {
		logger.Info("Existing session found, auto-connecting...")
		if err := whatsappService.Start(); err != nil {
			logger.Warnf("Auto-start failed: %v (user can manually start)", err)
		}
	} else {
		logger.Info("No existing session, waiting for user to start pairing")
	}

	// Create and start server (WebSocket RPC only)
	srv := server.New(whatsappService, logger)
	router := srv.SetupRoutes()

	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: router,
	}

	// Start server in goroutine
	go func() {
		logger.Infof("WebSocket RPC server listening on port %d", cfg.Server.Port)
		logger.Infof("Connect via: ws://localhost:%d/ws/rpc", cfg.Server.Port)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	whatsappService.Shutdown()

	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	logger.Info("Server stopped")
}
