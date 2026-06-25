package config

import "time"

type ResilienceConfig struct {
	Breaker BreakerConfig `yaml:"breaker" mapstructure:"breaker"`
	Retry   RetryConfig   `yaml:"retry" mapstructure:"retry"`
	Timeout TimeoutConfig `yaml:"timeout" mapstructure:"timeout"`
	Limiter LimiterConfig `yaml:"limiter" mapstructure:"limiter"`
}

type BreakerConfig struct {
	Enable           bool    `yaml:"enable" mapstructure:"enable"`
	Strategy         string  `yaml:"strategy" mapstructure:"strategy"`
	Threshold        float64 `yaml:"threshold" mapstructure:"threshold"`
	SlowCallRTT      string  `yaml:"slow_call_rtt" mapstructure:"slow_call_rtt"`
	SlowCallRatio    float64 `yaml:"slow_call_ratio" mapstructure:"slow_call_ratio"`
	RetryTimeoutMs   int     `yaml:"retry_timeout_ms" mapstructure:"retry_timeout_ms"`
	MinRequestAmount int64   `yaml:"min_request_amount" mapstructure:"min_request_amount"`
	StatIntervalMs   int     `yaml:"stat_interval_ms" mapstructure:"stat_interval_ms"`
}

type RetryConfig struct {
	Enable         bool    `yaml:"enable" mapstructure:"enable"`
	MaxAttempts    int     `yaml:"max_attempts" mapstructure:"max_attempts"`
	InitialBackoff string  `yaml:"initial_backoff" mapstructure:"initial_backoff"`
	MaxBackoff     string  `yaml:"max_backoff" mapstructure:"max_backoff"`
	Multiplier     float64 `yaml:"multiplier" mapstructure:"multiplier"`
	Jitter         float64 `yaml:"jitter" mapstructure:"jitter"`
}

type TimeoutConfig struct {
	Enable  bool   `yaml:"enable" mapstructure:"enable"`
	Default string `yaml:"default" mapstructure:"default"`
}

type LimiterConfig struct {
	Enable    bool    `yaml:"enable" mapstructure:"enable"`
	ClientQPS float64 `yaml:"client_qps" mapstructure:"client_qps"`
	ServerQPS float64 `yaml:"server_qps" mapstructure:"server_qps"`
}

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

const (
	defaultSlowCallRTT = 500 * time.Millisecond
	defaultInitialBackoff = 50 * time.Millisecond
	defaultMaxBackoff = 500 * time.Millisecond
	defaultTimeout = 3 * time.Second
)

func (c *BreakerConfig) SlowCallRTTDuration() time.Duration {
	d, err := time.ParseDuration(c.SlowCallRTT)
	if err != nil || d <= 0 {
		return defaultSlowCallRTT
	}
	return d
}

func (c *RetryConfig) InitialBackoffDuration() time.Duration {
	d, err := time.ParseDuration(c.InitialBackoff)
	if err != nil || d <= 0 {
		return defaultInitialBackoff
	}
	return d
}

func (c *RetryConfig) MaxBackoffDuration() time.Duration {
	d, err := time.ParseDuration(c.MaxBackoff)
	if err != nil || d <= 0 {
		return defaultMaxBackoff
	}
	return d
}

func (c *TimeoutConfig) DefaultDuration() time.Duration {
	d, err := time.ParseDuration(c.Default)
	if err != nil || d <= 0 {
		return defaultTimeout
	}
	return d
}
