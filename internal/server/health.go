package server

import (
	"net/http"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// HealthChecker 提供 gRPC 和 HTTP 双协议健康检查
type HealthChecker struct {
	grpcHealth *health.Server
	httpAddr   string
}

// NewHealthChecker 创建健康检查器
func NewHealthChecker(grpcHealth *health.Server, httpAddr string) *HealthChecker {
	return &HealthChecker{
		grpcHealth: grpcHealth,
		httpAddr:   httpAddr,
	}
}

// StartHTTP 启动 HTTP 健康检查端点
func (h *HealthChecker) StartHTTP() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return http.ListenAndServe(h.httpAddr, mux)
}

// SetServingStatus 设置服务状态
func (h *HealthChecker) SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus) {
	h.grpcHealth.SetServingStatus(service, status)
}
