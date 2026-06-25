package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewMonitorMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

func StartMonitorHTTP(addr string) error {
	return http.ListenAndServe(addr, NewMonitorMux())
}
