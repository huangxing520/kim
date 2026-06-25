// 文件：config.go
// 职责：Gateway 配置加载——通过 internal/config 从 YAML 文件加载 Gateway 服务配置。
//
// 定义的类型：
//   - Config 结构体：Gateway 配置（ServiceID / Listen / GRPCListen / ConsulURL / AppSecret / 协程池等）
//
// 方法：
//   - LoadConfig(path) → 加载配置并填充默认值

package gateway

import (
	"github.com/klintcheng/kim/internal/config"
	"github.com/klintcheng/kim/model"
)

// Config Gateway 服务配置
type Config struct {
	ServiceID       string                  `mapstructure:"service_id"`
	ServiceName     string                  `mapstructure:"service_name"`
	Listen          string                  `mapstructure:"listen"`
	GRPCListen      string                  `mapstructure:"grpc_listen"`
	GRPCPort        int                     `mapstructure:"grpc_port"`
	PublicAddress   string                  `mapstructure:"public_address"`
	PublicPort      int                     `mapstructure:"public_port"`
	Tags            []string                `mapstructure:"tags"`
	Domain          string                  `mapstructure:"domain"`
	ConsulURL       string                  `mapstructure:"consul_url"`
	MonitorPort     int                     `mapstructure:"monitor_port"`
	AppSecret       string                  `mapstructure:"app_secret"`
	LogLevel        string                  `mapstructure:"log_level"`
	MessageGPool    int                     `mapstructure:"message_g_pool"`
	ConnectionGPool int                     `mapstructure:"connection_g_pool"`
	Protocol        string                  `mapstructure:"protocol"`
	Kafka           model.KafkaSettings     `mapstructure:"kafka"`
	Resilience      config.ResilienceConfig `mapstructure:"resilience"`
	Trace           config.TraceConfig      `mapstructure:"trace"`
}

// LoadConfig 从指定路径加载配置
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := config.Load(path, &cfg); err != nil {
		return nil, err
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8000"
	}
	if cfg.GRPCListen == "" {
		cfg.GRPCListen = ":9001"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "wgateway"
	}
	if cfg.Protocol == "" {
		cfg.Protocol = "ws"
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.MonitorPort == 0 {
		cfg.MonitorPort = 8001
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
