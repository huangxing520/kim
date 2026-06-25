package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// MessageInTotal 按维度统计网关接收消息总数
	MessageInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "message_in_total",
		Help:      "gateway received message total",
	}, []string{"serviceId", "serviceName", "command"})

	// MessageInFlowBytes 按维度统计网关接收消息字节数
	MessageInFlowBytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "message_in_flow_bytes",
		Help:      "gateway received message bytes",
	}, []string{"serviceId", "serviceName", "command"})

	// NoServerFoundErrorTotal 查找zone分区中服务失败的次数
	NoServerFoundErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "no_server_found_error_total",
		Help:      "zone service lookup error total",
	}, []string{"zone"})

	// GRPCServerHandledTotal gRPC server 处理的请求总数
	GRPCServerHandledTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_server_handled_total",
		Help:      "gRPC server handled total",
	}, []string{"service", "method", "code"})

	// GRPCServerHandlingSeconds gRPC server 处理耗时
	GRPCServerHandlingSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "kim",
		Name:      "grpc_server_handling_seconds",
		Help:      "gRPC server handling seconds",
	}, []string{"service", "method"})

	// GRPCCircuitBreakerState 断路器状态：0=Closed, 1=Open, 2=HalfOpen
	GRPCCircuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "kim",
		Name:      "grpc_breaker_state",
		Help:      "circuit breaker state: 0=Closed, 1=Open, 2=HalfOpen",
	}, []string{"service", "instance", "method"})

	// GRPCRetryTotal 重试次数
	GRPCRetryTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_retry_total",
		Help:      "gRPC retry total",
	}, []string{"service", "method", "reason"})

	// GRPCRateLimitRejected 限流拒绝次数
	GRPCRateLimitRejected = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "kim",
		Name:      "grpc_ratelimit_rejected_total",
		Help:      "gRPC rate-limited total",
	}, []string{"side", "service", "method"})
)
