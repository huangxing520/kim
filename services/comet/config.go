package comet

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

// Config Comet 服务配置
type Config struct {
	ServiceID       string                  `mapstructure:"service_id"`
	Listen          string                  `mapstructure:"listen"`
	PublicAddress   string                  `mapstructure:"public_address"`
	PublicPort      int                     `mapstructure:"public_port"`
	MonitorPort     int                     `mapstructure:"monitor_port"`
	Tags            []string                `mapstructure:"tags"`
	Zone            string                  `mapstructure:"zone"`
	ConsulURL       string                  `mapstructure:"consul_url"`
	RedisAddrs      string                  `mapstructure:"redis_addrs"`
	RedisPassword   string                  `mapstructure:"redis_password"`
	LogLevel        string                  `mapstructure:"log_level"`
	MessageGPool    int                     `mapstructure:"message_g_pool"`
	ConnectionGPool int                     `mapstructure:"connection_g_pool"`
	Kafka           model.KafkaSettings     `mapstructure:"kafka"`
	Resilience      config.ResilienceConfig `mapstructure:"resilience"`
	Trace           config.TraceConfig      `mapstructure:"trace"`
	GRPC            config.GRPCConfig       `mapstructure:"grpc"`
	AppSecret       string                  `mapstructure:"app_secret"`
}

// LoadConfig 从指定路径加载配置
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := config.Load(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8005"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MonitorPort == 0 {
		cfg.MonitorPort = 8007
	}
	// 合并弹性配置默认值
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable {
		cfg.Resilience = defaults
	}
	// 合并追踪配置默认值（不覆盖 Enable，仅填充缺失字段）
	traceDefaults := config.DefaultTraceConfig()
	if cfg.Trace.Exporter == "" {
		cfg.Trace.Exporter = traceDefaults.Exporter
	}
	if cfg.Trace.Endpoint == "" {
		cfg.Trace.Endpoint = traceDefaults.Endpoint
	}
	if cfg.Trace.SamplingRatio == 0 {
		cfg.Trace.SamplingRatio = traceDefaults.SamplingRatio
	}
	return &cfg, nil
}
