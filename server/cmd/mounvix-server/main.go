package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

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
	if err := http.ListenAndServe(*addr, a); err != nil {
		log.Fatal(err)
	}
}
