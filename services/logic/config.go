package logic

import (
	"fmt"

	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

type Config struct {
	ServiceID     string                  `mapstructure:"service_id"`
	NodeID        int64                   `mapstructure:"node_id"`
	Listen        string                  `mapstructure:"listen"`
	PublicAddress string                  `mapstructure:"public_address"`
	PublicPort    int                     `mapstructure:"public_port"`
	MonitorPort   int                     `mapstructure:"monitor_port"`
	Tags          []string                `mapstructure:"tags"`
	ConsulURL     string                  `mapstructure:"consul_url"`
	RedisAddrs    string                  `mapstructure:"redis_addrs"`
	Driver        string                  `mapstructure:"driver"`
	BaseDb        string                  `mapstructure:"base_db"`
	MessageDb     string                  `mapstructure:"message_db"`
	LogLevel      string                  `mapstructure:"log_level"`
	AppSecret     string                  `mapstructure:"app_secret"`
	Kafka         model.KafkaSettings     `mapstructure:"kafka"`
	Resilience    config.ResilienceConfig `mapstructure:"resilience"`
	Trace         config.TraceConfig      `mapstructure:"trace"`
	GRPC          config.GRPCConfig       `mapstructure:"grpc"`
}

func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := config.Load(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":9002"
	}
	if cfg.Driver == "" {
		cfg.Driver = "mysql"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MonitorPort == 0 {
		cfg.MonitorPort = 8009
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
	if cfg.AppSecret == "" {
		return nil, fmt.Errorf("app_secret is required in config")
	}
	return &cfg, nil
}
