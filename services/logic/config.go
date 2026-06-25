package logic

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

type Config struct {
	ServiceID     string              `mapstructure:"service_id"`
	NodeID        int64               `mapstructure:"node_id"`
	Listen        string              `mapstructure:"listen"`
	PublicAddress string              `mapstructure:"public_address"`
	PublicPort    int                 `mapstructure:"public_port"`
	Tags          []string            `mapstructure:"tags"`
	ConsulURL     string              `mapstructure:"consul_url"`
	RedisAddrs    string              `mapstructure:"redis_addrs"`
	Driver        string              `mapstructure:"driver"`
	BaseDb        string              `mapstructure:"base_db"`
	MessageDb     string              `mapstructure:"message_db"`
	LogLevel      string              `mapstructure:"log_level"`
	Kafka         model.KafkaSettings `mapstructure:"kafka"`
	Resilience    config.ResilienceConfig `mapstructure:"resilience"`
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
	// 合并弹性配置默认值
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable {
		cfg.Resilience = defaults
	}
	return &cfg, nil
}
