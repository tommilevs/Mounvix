package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/app"
	"github.com/tommilevs/Mounvix/server/internal/version"
)

func main() {
	configPath := flag.String("config", "", "path to config.json (default <data>/config.json)")
	dataDir := flag.String("data", "", "data dir override")
	addr := flag.String("addr", "", "listen address (default :<config.port>)")
	flag.Parse()

	cfg, cfgPath, err := app.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *dataDir != "" {
		cfg.DataDir = *dataDir
		if *configPath == "" {
			cfgPath = filepath.Join(cfg.DataDir, "config.json")
		}
	}
	a, err := app.New(cfg, cfgPath)
	if err != nil {
		log.Fatalf("app: %v", err)
	}
	if *addr == "" {
		*addr = fmt.Sprintf(":%d", cfg.Port)
	}
	log.Printf("mounvix-server %s on %s (data=%s)", version.Version, *addr, cfg.DataDir)
	server := &http.Server{Addr: *addr, Handler: a}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.ListenAndServe() }()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case sig := <-signals:
		log.Printf("shutdown requested: %s", sig)
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			log.Printf("http server: %v", err)
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown: %v", err)
	}
	if err := a.Close(); err != nil {
		log.Printf("app shutdown: %v", err)
	}
}
