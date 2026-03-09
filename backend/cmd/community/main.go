package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kolapsis/ackify/backend/internal/infrastructure/database"
	"github.com/kolapsis/ackify/backend/internal/infrastructure/tenant"
	"github.com/kolapsis/ackify/backend/pkg/config"
	"github.com/kolapsis/ackify/backend/pkg/logger"
	"github.com/kolapsis/ackify/backend/pkg/web"
)

// Build-time variables set via ldflags
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

//go:embed all:web/dist
var frontend embed.FS

func main() {
	// Handle health check subcommand for Docker HEALTHCHECK
	if len(os.Args) > 1 && os.Args[1] == "health" {
		os.Exit(runHealthCheck())
	}

	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	logger.SetLevelAndFormat(logger.ParseLevel(cfg.Logger.Level), cfg.Logger.Format)
	logger.Logger.Info("Starting Ackify Community Edition",
		"version", Version,
		"commit", Commit,
		"build_date", BuildDate,
		"telemetry", cfg.Telemetry.Enabled)

	db, err := database.InitDB(ctx, database.Config{DSN: cfg.Database.DSN})
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	tenantProvider, err := tenant.NewSingleTenantProviderWithContext(ctx, db)
	if err != nil {
		log.Fatalf("failed to initialize tenant provider: %v", err)
	}

	// === Build Server ===
	// All services (I18n, Email, MagicLink, Config, Session) and
	// default providers (DynamicAuthProvider, SimpleAuthorizer) are created internally.
	server, err := web.NewServerBuilder(cfg, frontend, Version).
		WithDB(db).
		WithTenantProvider(tenantProvider).
		Build(ctx)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	go func() {
		log.Printf("Community Edition server starting on %s", server.GetAddr())
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down Community Edition server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Community Edition server exited")
}

// runHealthCheck performs a health check against the local server.
// Returns 0 on success, 1 on failure.
func runHealthCheck() int {
	addr := os.Getenv("ACKIFY_LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	// Build health URL (handle both ":8080" and "0.0.0.0:8080" formats)
	host := "localhost"
	port := addr
	if addr[0] != ':' {
		// Format is "host:port", extract port
		for i := len(addr) - 1; i >= 0; i-- {
			if addr[i] == ':' {
				port = addr[i:]
				break
			}
		}
	}

	url := fmt.Sprintf("http://%s%s/api/v1/health", host, port)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Health check failed: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "Health check failed: status %d\n", resp.StatusCode)
		return 1
	}

	return 0
}
