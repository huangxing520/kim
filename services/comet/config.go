package comet

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

// Config Comet 服务配置
type Config struct {
	ServiceID       string              `mapstructure:"service_id"`
	Listen          string              `mapstructure:"listen"`
	PublicAddress   string              `mapstructure:"public_address"`
	PublicPort      int                 `mapstructure:"public_port"`
	Tags            []string            `mapstructure:"tags"`
	Zone            string              `mapstructure:"zone"`
	ConsulURL       string              `mapstructure:"consul_url"`
	RedisAddrs      string              `mapstructure:"redis_addrs"`
	LogLevel        string              `mapstructure:"log_level"`
	MessageGPool    int                 `mapstructure:"message_g_pool"`
	ConnectionGPool int                 `mapstructure:"connection_g_pool"`
	Kafka           model.KafkaSettings `mapstructure:"kafka"`
	Resilience      config.ResilienceConfig `mapstructure:"resilience"`
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
	// 合并弹性配置默认值
	defaults := config.DefaultResilienceConfig()
	if !cfg.Resilience.Breaker.Enable {
		cfg.Resilience = defaults
	}
	return &cfg, nil
}
