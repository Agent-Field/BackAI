// af-stack — the suite runtime entry point.
//
// Reads config (yaml + env), starts the HTTP server, drains on SIGINT/SIGTERM.
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

	"github.com/Agent-Field/backai/services/runtime/internal/config"
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

	srv := server.New(cfg, log)
	if err := srv.Start(ctx); err != nil {
		log.Error("server stopped with error", "error", err)
		os.Exit(1)
	}
	log.Info("af-stack exited cleanly")
}
