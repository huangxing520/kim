package config

import "time"

// ResilienceConfig 弹性套件配置（代码默认 + YAML 覆盖）
type ResilienceConfig struct {
	Breaker BreakerConfig `yaml:"breaker" mapstructure:"breaker"`
	Retry   RetryConfig   `yaml:"retry" mapstructure:"retry"`
	Timeout TimeoutConfig `yaml:"timeout" mapstructure:"timeout"`
	Limiter LimiterConfig `yaml:"limiter" mapstructure:"limiter"`
}

// BreakerConfig 断路器配置
type BreakerConfig struct {
	Enable           bool    `yaml:"enable" mapstructure:"enable"`
	Strategy         string  `yaml:"strategy" mapstructure:"strategy"` // "error_rate" | "slow_call" | "both"
	Threshold        float64 `yaml:"threshold" mapstructure:"threshold"`
	SlowCallRTT      string  `yaml:"slow_call_rtt" mapstructure:"slow_call_rtt"`
	SlowCallRatio    float64 `yaml:"slow_call_ratio" mapstructure:"slow_call_ratio"`
	RetryTimeoutMs   int     `yaml:"retry_timeout_ms" mapstructure:"retry_timeout_ms"`
	MinRequestAmount int64   `yaml:"min_request_amount" mapstructure:"min_request_amount"`
	StatIntervalMs   int     `yaml:"stat_interval_ms" mapstructure:"stat_interval_ms"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	Enable         bool    `yaml:"enable" mapstructure:"enable"`
	MaxAttempts    int     `yaml:"max_attempts" mapstructure:"max_attempts"`
	InitialBackoff string  `yaml:"initial_backoff" mapstructure:"initial_backoff"`
	MaxBackoff     string  `yaml:"max_backoff" mapstructure:"max_backoff"`
	Multiplier     float64 `yaml:"multiplier" mapstructure:"multiplier"`
	Jitter         float64 `yaml:"jitter" mapstructure:"jitter"`
}

// TimeoutConfig 超时配置
type TimeoutConfig struct {
	Enable  bool   `yaml:"enable" mapstructure:"enable"`
	Default string `yaml:"default" mapstructure:"default"`
}

// LimiterConfig 限流配置
type LimiterConfig struct {
	Enable    bool    `yaml:"enable" mapstructure:"enable"`
	ClientQPS float64 `yaml:"client_qps" mapstructure:"client_qps"`
	ServerQPS float64 `yaml:"server_qps" mapstructure:"server_qps"`
}

// DefaultResilienceConfig 返回内置默认值
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		Breaker: BreakerConfig{
			Enable:           true,
			Strategy:         "both",
			Threshold:        0.5,
			SlowCallRTT:      "500ms",
			SlowCallRatio:    0.5,
			RetryTimeoutMs:   5000,
			MinRequestAmount: 10,
			StatIntervalMs:   1000,
		},
		Retry: RetryConfig{
			Enable:         true,
			MaxAttempts:    3,
			InitialBackoff: "50ms",
			MaxBackoff:     "500ms",
			Multiplier:     2.0,
			Jitter:         0.1,
		},
		Timeout: TimeoutConfig{
			Enable:  true,
			Default: "3s",
		},
		Limiter: LimiterConfig{
			Enable:    true,
			ClientQPS: 100,
			ServerQPS: 200,
		},
	}
}

// SlowCallRTTDuration 解析慢调用阈值
func (c *BreakerConfig) SlowCallRTTDuration() time.Duration {
	d, _ := time.ParseDuration(c.SlowCallRTT)
	return d
}

// InitialBackoffDuration 解析初始退避
func (c *RetryConfig) InitialBackoffDuration() time.Duration {
	d, _ := time.ParseDuration(c.InitialBackoff)
	return d
}

// MaxBackoffDuration 解析最大退避
func (c *RetryConfig) MaxBackoffDuration() time.Duration {
	d, _ := time.ParseDuration(c.MaxBackoff)
	return d
}

// DefaultDuration 解析默认超时
func (c *TimeoutConfig) DefaultDuration() time.Duration {
	d, _ := time.ParseDuration(c.Default)
	return d
}
