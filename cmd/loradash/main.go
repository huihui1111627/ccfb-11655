// Command loradash starts the LoRa network simulation server and serves the
// dashboard plus the JSON API.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"lorasim/internal/web"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	seed := flag.Int64("seed", 20261119, "initial scenario seed")
	nodes := flag.Int("nodes", 300, "initial node count")
	hours := flag.Int("hours", 6, "initial simulated duration in hours")
	flag.Parse()

	start := time.Now()
	srv := web.NewServer(*seed, *nodes, *hours)
	log.Printf("initial simulation ready in %s", time.Since(start).Round(time.Millisecond))

	log.Printf("LoRa dashboard listening on http://localhost%s", *addr)
	if err := http.ListenAndServe(*addr, srv.Handler()); err != nil {
		log.Fatal(fmt.Errorf("server: %w", err))
	}
}
