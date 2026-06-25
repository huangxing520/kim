package router

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

type Config struct {
	ServiceID     string                  `mapstructure:"service_id"`
	Listen        string                  `mapstructure:"listen"`
	PublicAddress string                  `mapstructure:"public_address"`
	PublicPort    int                     `mapstructure:"public_port"`
	MonitorPort   int                     `mapstructure:"monitor_port"`
	ConsulURL     string                  `mapstructure:"consul_url"`
	LogLevel      string                  `mapstructure:"log_level"`
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
		cfg.Listen = ":8100"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable {
		cfg.Resilience = defaults
	}
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
