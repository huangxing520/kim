package client

import (
	"strings"
	"sync"

	sentinel "github.com/alibaba/sentinel-golang/api"
	"github.com/alibaba/sentinel-golang/core/circuitbreaker"
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/internal/logger"
	"github.com/klintcheng/kim/internal/metrics"
)

var (
	sentinelOnce sync.Once
	sentinelErr  error
	breakerRules sync.Map // resource -> bool，防止重复注册
)

// InitSentinel 初始化 Sentinel 全局配置（进程级一次）
func InitSentinel() error {
	sentinelOnce.Do(func() {
		sentinelErr = sentinel.InitDefault()
		if sentinelErr == nil {
			circuitbreaker.RegisterStateChangeListeners(&breakerStateListener{})
		}
	})
	return sentinelErr
}

// ensureBreaker 为 resource=<svc>:<inst>:<method> 注册断路器规则（幂等）
func ensureBreaker(resource string, cfg config.BreakerConfig) {
	if !cfg.Enable {
		return
	}
	if _, loaded := breakerRules.LoadOrStore(resource, true); loaded {
		return
	}
	slowRTT := cfg.SlowCallRTTDuration()
	rules := make([]*circuitbreaker.Rule, 0, 2)
	if cfg.Strategy == "error_rate" || cfg.Strategy == "both" {
		rules = append(rules, &circuitbreaker.Rule{
			Resource:         resource,
			Strategy:         circuitbreaker.ErrorRatio,
			Threshold:        cfg.Threshold,
			RetryTimeoutMs:   uint32(cfg.RetryTimeoutMs),
			MinRequestAmount: uint64(cfg.MinRequestAmount),
			StatIntervalMs:   uint32(cfg.StatIntervalMs),
		})
	}
	if cfg.Strategy == "slow_call" || cfg.Strategy == "both" {
		rules = append(rules, &circuitbreaker.Rule{
			Resource:         resource,
			Strategy:         circuitbreaker.SlowRequestRatio,
			MaxAllowedRtMs:   uint64(slowRTT.Milliseconds()),
			Threshold:        cfg.SlowCallRatio,
			RetryTimeoutMs:   uint32(cfg.RetryTimeoutMs),
			MinRequestAmount: uint64(cfg.MinRequestAmount),
			StatIntervalMs:   uint32(cfg.StatIntervalMs),
		})
	}
	if _, err := circuitbreaker.LoadRules(rules); err != nil {
		logger.CommonLogger.Errorf("load breaker rules for %s: %v", resource, err)
	}
}

// parseResource 从 "logic:logic-1:InsertUserMessage" 解析出 service/instance/method
func parseResource(resource string) (service, instance, method string) {
	parts := strings.SplitN(resource, ":", 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// breakerStateListener 推送断路器状态变更到日志 + Prometheus
type breakerStateListener struct{}

func (l *breakerStateListener) OnTransformToClosed(prev circuitbreaker.State, rule circuitbreaker.Rule) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Infof("circuit breaker CLOSED: %s", rule.Resource)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(0)
}

func (l *breakerStateListener) OnTransformToOpen(prev circuitbreaker.State, rule circuitbreaker.Rule, snapshot interface{}) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Warnf("circuit breaker OPEN: %s, snapshot: %v", rule.Resource, snapshot)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(1)
}

func (l *breakerStateListener) OnTransformToHalfOpen(prev circuitbreaker.State, rule circuitbreaker.Rule) {
	svc, inst, mtd := parseResource(rule.Resource)
	logger.CommonLogger.Infof("circuit breaker HALF_OPEN: %s", rule.Resource)
	metrics.GRPCCircuitBreakerState.WithLabelValues(svc, inst, mtd).Set(2)
}
