// Package trace 链路追踪初始化——封装 OpenTelemetry TracerProvider + OTLP exporter。
//
// 提供 InitTrace(serviceName, cfg) 在各服务启动时初始化全局 TracerProvider，
// 返回 shutdown 函数用于优雅关闭 exporter。
package trace

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// InitTrace 初始化全局 TracerProvider，返回 shutdown 函数
// 若 cfg.Enable=false，返回 noop shutdown 且不做任何操作
func InitTrace(serviceName string, cfg config.TraceConfig) (func(), error) {
	if !cfg.Enable {
		return func() {}, nil
	}

	exporter, err := newExporter(cfg)
	if err != nil {
		return func() {}, fmt.Errorf("create trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		return func() {}, fmt.Errorf("create trace resource: %w", err)
	}

	samplingRatio := cfg.SamplingRatio
	if samplingRatio <= 0 {
		samplingRatio = 1.0
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(samplingRatio)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	logger.CommonLogger.Infof("trace enabled: service=%s exporter=%s endpoint=%s ratio=%.2f",
		serviceName, cfg.Exporter, cfg.Endpoint, samplingRatio)

	shutdown := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := tp.Shutdown(ctx); err != nil {
			logger.CommonLogger.Warnf("trace shutdown: %v", err)
		}
	}
	return shutdown, nil
}

// newExporter 按 cfg.Exporter 类型创建 exporter
func newExporter(cfg config.TraceConfig) (sdktrace.SpanExporter, error) {
	switch cfg.Exporter {
	case "noop":
		return noopExporter{}, nil
	case "stdout":
		return stdouttrace.New()
	case "otlp", "":
		return newOTLPExporter(cfg)
	default:
		return nil, fmt.Errorf("unsupported exporter: %s", cfg.Exporter)
	}
}

// newOTLPExporter 创建 OTLP gRPC exporter
func newOTLPExporter(cfg config.TraceConfig) (sdktrace.SpanExporter, error) {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = "127.0.0.1:4317"
	}

	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracegrpc.WithDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return otlptracegrpc.New(ctx, opts...)
}

// noopExporter 空实现
type noopExporter struct{}

func (noopExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	return nil
}
func (noopExporter) Shutdown(ctx context.Context) error { return nil }

// ErrTraceDisabled 链路追踪未启用
var ErrTraceDisabled = errors.New("trace disabled")
