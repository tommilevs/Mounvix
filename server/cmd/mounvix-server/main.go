package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/tommilevs/Mounvix/server/internal/version"
)

var started = time.Now()

func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"version":  version.Version,
			"uptime_s": int(time.Since(started).Seconds()),
		})
	})
	return mux
}

func main() {
	addr := flag.String("addr", ":48100", "listen address")
	flag.Parse()
	log.Printf("mounvix-server %s listening on %s", version.Version, *addr)
	if err := http.ListenAndServe(*addr, newMux()); err != nil {
		log.Fatal(err)
	}
}
