# config 模块

## 模块概述
基于 Viper 的配置加载模块，支持 YAML 配置文件读取与 `KIM_` 前缀环境变量覆盖，提供 gRPC、链路追踪、弹性套件等内置配置结构。

## 架构设计
配置模块采用「代码默认值 + YAML 文件覆盖 + 环境变量最高优先级」的三层配置策略。核心函数 `Load` 读取指定路径的 YAML 文件，自动绑定 `KIM_` 前缀环境变量，最终反序列化到用户传入的结构体中。模块同时内置了 gRPC、链路追踪、弹性防护（断路器/重试/超时/限流）等通用配置结构及默认值。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Load` | 函数 | 加载配置文件并绑定环境变量 |
| `GRPCConfig` | 结构体 | gRPC 服务端/客户端配置（TLS、认证、反射） |
| `TraceConfig` | 结构体 | OpenTelemetry 链路追踪配置 |
| `ResilienceConfig` | 结构体 | 弹性套件配置（断路器、重试、超时、限流） |
| `BreakerConfig` | 结构体 | 断路器配置 |
| `RetryConfig` | 结构体 | 重试策略配置 |
| `TimeoutConfig` | 结构体 | 超时配置 |
| `LimiterConfig` | 结构体 | 限流配置 |

## 核心接口

```go
// Load 加载配置文件 + 环境变量覆盖
func Load(path string, out interface{}) error

// DefaultGRPCConfig 返回 gRPC 默认配置
func DefaultGRPCConfig() GRPCConfig

// DefaultTraceConfig 返回链路追踪默认配置（默认关闭）
func DefaultTraceConfig() TraceConfig

// DefaultResilienceConfig 返回弹性套件默认配置
func DefaultResilienceConfig() ResilienceConfig
```

## 配置说明

### 环境变量规则
- 前缀：`KIM_`
- 分隔符：配置中的 `.` 替换为 `_`
- 示例：`KIM_CONSUL_URL` 对应配置键 `consul.url`
- 切片类型：`kafka.brokers` 通过空格分隔的环境变量 `KIM_KAFKA_BROKERS` 设置

### GRPCConfig 字段
| 字段 | YAML 键 | 类型 | 默认值 | 说明 |
|------|---------|------|--------|------|
| TLSEnable | `tls_enable` | bool | false | 是否启用 TLS |
| TLSCertFile | `tls_cert_file` | string | "" | 证书文件路径 |
| TLSKeyFile | `tls_key_file` | string | "" | 私钥文件路径 |
| TLSCAFile | `tls_ca_file` | string | "" | CA 证书路径 |
| AuthEnable | `auth_enable` | bool | false | 是否启用 Token 认证 |
| Reflection | `reflection` | bool | false | 是否启用 gRPC reflection |

### TraceConfig 字段
| 字段 | YAML 键 | 类型 | 默认值 | 说明 |
|------|---------|------|--------|------|
| Enable | `enable` | bool | false | 是否启用链路追踪 |
| Exporter | `exporter` | string | "otlp" | 导出器类型：otlp/stdout/noop |
| Endpoint | `endpoint` | string | "127.0.0.1:4317" | OTLP gRPC 端点 |
| SamplingRatio | `sampling_ratio` | float64 | 0.1 | 采样率（0.0~1.0） |
| Insecure | `insecure` | bool | true | 是否使用明文连接 |

### ResilienceConfig 字段
- **Breaker（断路器）**：支持 `error_rate`（错误率）、`slow_call`（慢调用比例）、`both`（两者）三种策略
- **Retry（重试）**：指数退避 + 抖动，默认最多 3 次，初始 50ms，最大 500ms
- **Timeout（超时）**：默认 3 秒，若 context 已设置 deadline 则不覆盖
- **Limiter（限流）**：客户端默认 100 QPS，服务端默认 200 QPS

## 使用示例

```go
package main

import (
    "github.com/klintcheng/kim/internal/config"
)

type MyServiceConfig struct {
    ConsulURL string `yaml:"consul_url"`
    GRPC      config.GRPCConfig `yaml:"grpc"`
    Trace     config.TraceConfig `yaml:"trace"`
    Resilience config.ResilienceConfig `yaml:"resilience"`
}

func main() {
    var cfg MyServiceConfig
    if err := config.Load("conf.yaml", &cfg); err != nil {
        panic(err)
    }
    // 使用 cfg
}
```

## 依赖关系
- `github.com/spf13/viper` - 配置加载库
