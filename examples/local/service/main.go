// Command service is the HTTP workload bundled with the local deployment example.
package main

import (
	"flag"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

func main() {
	port := flag.Int("port", 8080, "local HTTP service port")
	flag.Parse()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "ok\n"); err != nil {
			log.Print("health response write failed")
		}
	})
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := io.WriteString(w, "Hello from deployd's local example!\n"); err != nil {
			log.Print("service response write failed")
		}
	})
	server := &http.Server{
		Addr:              "127.0.0.1:" + strconv.Itoa(*port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("example HTTP service exited")
	}
}
