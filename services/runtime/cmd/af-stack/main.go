// af-stack — the suite runtime entry point.
//
// Reads config (yaml + env), opens PG, connects to AF, starts the HTTP
// server, drains on SIGINT/SIGTERM.
//
// Usage:
//
//	af-stack            # serve with default config.yaml + env
//	af-stack version    # print version
//	af-stack -c path/to/config.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Agent-Field/backai/services/runtime/internal/agentfield"
	"github.com/Agent-Field/backai/services/runtime/internal/config"
	"github.com/Agent-Field/backai/services/runtime/internal/db"
	"github.com/Agent-Field/backai/services/runtime/internal/logger"
	"github.com/Agent-Field/backai/services/runtime/internal/server"
)

const version = "0.0.1"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Printf("af-stack %s\n", version)
			return
		}
	}

	var configPath string
	flag.StringVar(&configPath, "c", "", "path to config.yaml (default: ./config.yaml if exists)")
	flag.StringVar(&configPath, "config", "", "path to config.yaml (default: ./config.yaml if exists)")
	flag.Parse()

	if configPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			configPath = "config.yaml"
		}
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.Logging.Level, cfg.Logging.Format)
	log.Info("af-stack starting",
		"version", version,
		"http_addr", cfg.Server.HTTPAddr,
		"agentfield_url", cfg.AgentField.URL,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Optional: connect to Postgres. If DATABASE_URL is empty, the runtime
	// still boots — it just reports the DB as not-configured in /health.
	var database *db.DB
	if cfg.Database.URL != "" {
		dbCtx, dbCancel := context.WithTimeout(ctx, 30*time.Second)
		database, err = db.Open(dbCtx, db.Config{
			URL:             cfg.Database.URL,
			MaxConnections:  cfg.Database.MaxConnections,
			MaxIdleConns:    cfg.Database.MaxIdleConns,
			ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		})
		dbCancel()
		if err != nil {
			log.Error("database connection failed; continuing in degraded mode",
				"url_redacted", "(redacted)", "error", err)
		} else {
			log.Info("database connected")
			migCtx, migCancel := context.WithTimeout(ctx, 60*time.Second)
			if err := database.Migrate(migCtx); err != nil {
				log.Error("migrations failed", "error", err)
				database.Close()
				migCancel()
				os.Exit(1)
			}
			migCancel()
			log.Info("migrations applied")
			defer database.Close()
		}
	} else {
		log.Warn("database URL not configured; running without persistent state")
	}

	// AF client. Always constructed; health is reported through /health.
	afClient := agentfield.New(agentfield.Config{
		URL:            cfg.AgentField.URL,
		RequestTimeout: cfg.AgentField.RequestTimeout,
	})
	// Async probe to log AF reachability at startup.
	go func() {
		probeCtx, probeCancel := context.WithTimeout(ctx, cfg.AgentField.HealthTimeout)
		defer probeCancel()
		if _, err := afClient.Health(probeCtx); err != nil {
			log.Warn("agentfield not reachable at startup",
				"url", afClient.BaseURL(),
				"error", err,
			)
		} else {
			log.Info("agentfield healthy", "url", afClient.BaseURL())
		}
	}()

	srv := server.New(cfg, log, server.Deps{
		DB: database,
		AF: afClient,
	})
	if err := srv.Start(ctx); err != nil {
		log.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
	log.Info("af-stack exited cleanly")
}
