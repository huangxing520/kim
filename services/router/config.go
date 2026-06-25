// 文件：config.go
// 职责：Router 服务配置加载——通过 internal/config 从 YAML 文件加载 Router 服务配置。
//
// 定义的类型：
//   - Config 结构体：Router 配置（Listen / ConsulURL / LogLevel / Kafka）
//
// 方法：
//   - LoadConfig(path) → 加载配置并填充默认值

package router

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

// Config Router 服务配置
type Config struct {
	Listen    string              `mapstructure:"listen"`
	ConsulURL string              `mapstructure:"consul_url"`
	LogLevel  string              `mapstructure:"log_level"`
	Kafka     model.KafkaSettings `mapstructure:"kafka"`
}

// LoadConfig 从指定路径加载配置
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
	return &cfg, nil
}
