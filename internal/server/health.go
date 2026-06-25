package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewMonitorMux(grpcSrv *GRPCServer) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/health/live", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if grpcSrv != nil && grpcSrv.IsReady() {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("not ready"))
	})
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

func StartMonitorHTTP(addr string) error {
	return http.ListenAndServe(addr, NewMonitorMux(nil))
}

func StartMonitorHTTPWithReady(addr string, grpcSrv *GRPCServer) error {
	return http.ListenAndServe(addr, NewMonitorMux(grpcSrv))
}
