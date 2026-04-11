package main

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	"edgymeow/src/go/config"
	server "edgymeow/src/go/rpc"
	"edgymeow/src/go/whatsapp"
)

// discoverDNSServers reads nameservers from /etc/resolv.conf and returns them as host:53 pairs.
func discoverDNSServers() []string {
	var servers []string
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "nameserver") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				ip := fields[1]
				// Skip loopback — Go's default already tried that and failed
				if ip == "::1" || ip == "127.0.0.1" {
					continue
				}
				servers = append(servers, net.JoinHostPort(ip, "53"))
			}
		}
	}
	return servers
}

func main() {
	// Android DNS resolver: Go's default resolver tries [::1]:53 which fails on Android.
	// Dynamically discover DNS servers from resolv.conf, fall back to public DNS.
	if runtime.GOOS == "android" || os.Getenv("EDGYMEOW_ANDROID") == "1" {
		dnsServers := discoverDNSServers()
		// Append public DNS as fallback
		dnsServers = append(dnsServers, "8.8.8.8:53", "1.1.1.1:53")
		net.DefaultResolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{Timeout: 3 * time.Second}
				for _, dns := range dnsServers {
					if conn, err := d.DialContext(ctx, "udp", dns); err == nil {
						return conn, nil
					}
				}
				return d.DialContext(ctx, network, address)
			},
		}
	}

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
	whatsappService, err := whatsapp.NewService(cfg, logger)
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
		Addr:    fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
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
