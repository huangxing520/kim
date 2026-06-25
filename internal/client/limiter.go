package client

import (
	"sync"

	"github.com/alibaba/sentinel-golang/core/flow"
	"github.com/klintcheng/kim/internal/logger"
)

var limiterRules sync.Map // resource -> bool，防止重复注册

// ensureLimiter 为 resource 注册限流规则（幂等）
// 被 limiterInterceptor 调用
func ensureLimiter(resource string, qps float64) {
	if _, loaded := limiterRules.LoadOrStore(resource, true); loaded {
		return
	}
	_, err := flow.LoadRules([]*flow.Rule{
		{
			Resource:               resource,
			Threshold:              qps,
			TokenCalculateStrategy: flow.Direct,
			ControlBehavior:        flow.Reject,
		},
	})
	if err != nil {
		logger.CommonLogger.Errorf("load limiter rules for %s: %v", resource, err)
	}
}
