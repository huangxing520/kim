package config

// TraceConfig 链路追踪配置（默认关闭，YAML 覆盖）
type TraceConfig struct {
	Enable        bool    `yaml:"enable" mapstructure:"enable"`
	Exporter      string  `yaml:"exporter" mapstructure:"exporter"`           // "otlp" | "stdout" | "noop"
	Endpoint      string  `yaml:"endpoint" mapstructure:"endpoint"`           // OTLP gRPC endpoint，如 "127.0.0.1:4317"
	SamplingRatio float64 `yaml:"sampling_ratio" mapstructure:"sampling_ratio"` // 0.0~1.0
	Insecure      bool    `yaml:"insecure" mapstructure:"insecure"`           // OTLP 是否用 plaintext
}

// DefaultTraceConfig 返回内置默认值（默认关闭）
func DefaultTraceConfig() TraceConfig {
	return TraceConfig{
		Enable:        false,
		Exporter:      "otlp",
		Endpoint:      "127.0.0.1:4317",
		SamplingRatio: 0.1,
		Insecure:      true,
	}
}
