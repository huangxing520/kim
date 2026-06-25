# naming 模块

## 模块概述
基于 Consul 的服务注册与发现模块，提供服务注册/注销、实例查询、变更订阅能力，并实现 gRPC `resolver.Builder` 接口以支持 gRPC 客户端通过 `consul://` scheme 自动发现服务实例。

## 架构设计
模块采用接口抽象 + Consul 具体实现的设计模式。核心 `Naming` 接口定义了服务注册与发现的标准操作，`ConsulNaming` 基于 Consul Catalog API 实现。服务变更订阅通过 Consul 长轮询（WaitIndex）机制实现：后台 goroutine 循环调用 Catalog API 并携带 WaitIndex，服务列表无变化时阻塞等待，有变化时立即返回并触发回调。同时提供 `ConsulResolverBuilder` 实现 gRPC resolver 接口，将服务发现能力无缝集成到 gRPC 客户端连接中。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Naming` | 接口 | 服务注册与发现抽象接口 |
| `DefaultService` | 结构体 | `ServiceRegistration` 接口的默认实现 |
| `ConsulNaming` | 结构体 | Naming 接口的 Consul 实现，支持长轮询 watch |
| `Watch` | 结构体 | 单次服务订阅状态（回调、WaitIndex、退出通道） |
| `ConsulResolverBuilder` | 结构体 | gRPC resolver builder，scheme 为 "consul" |
| `consulResolver` | 结构体 | gRPC resolver 实现，订阅服务变更并更新地址 |

### Meta Key 常量
| 常量 | 值 | 说明 |
|------|-----|------|
| `KeyProtocol` | "protocol" | Consul ServiceMeta 中协议字段 key |
| `KeyHealthURL` | "health_url" | Consul ServiceMeta 中 HTTP 健康检查 URL key |

## 核心接口

```go
// Naming 服务注册与发现接口
type Naming interface {
    Find(serviceName string, tags ...string) ([]kim.ServiceRegistration, error)
    Subscribe(serviceName string, callback func(services []kim.ServiceRegistration)) error
    Unsubscribe(serviceName string) error
    Register(service kim.ServiceRegistration) error
    Deregister(serviceID string) error
}

// 创建 ConsulNaming 实例
func NewNaming(consulUrl string) (*ConsulNaming, error)

// 创建 DefaultService 实例
func NewEntry(id, name, protocol string, address string, port int) kim.ServiceRegistration

// gRPC resolver builder
func NewConsulResolverBuilder(ns *ConsulNaming) *ConsulResolverBuilder
func (b *ConsulResolverBuilder) Scheme() string  // 返回 "consul"
```

## 配置说明

### 服务注册时自动配置
- **健康检查**：若 ServiceMeta 中包含 `health_url`，自动注册 HTTP 健康检查
  - 检查间隔：10 秒
  - 超时：1 秒
  - 不健康 20 秒后自动注销
- **协议标记**：自动在 ServiceMeta 中添加 `protocol` 字段

### 长轮询特性
- 查询超时：5 分钟（避免永久阻塞）
- 缓存：启用 Consul 缓存，MaxAge 1 分钟
- 健康过滤：仅返回 AggregatedStatus 为 HealthPassing 的实例
- 取消机制：通过 context.Cancel 中断阻塞的 HTTP 请求

## 使用示例

### 初始化 Consul 服务发现
```go
ns, err := naming.NewNaming("127.0.0.1:8500")
if err != nil {
    panic(err)
}
```

### 注册服务
```go
svc := naming.NewEntry(
    "gateway-1",
    "gateway",
    "tcp",
    "127.0.0.1",
    9001,
)
// 可设置 Meta（含 health_url 用于 Consul 健康检查）
svc.(*naming.DefaultService).Meta = map[string]string{
    naming.KeyHealthURL: "http://127.0.0.1:8000/health",
}
if err := ns.Register(svc); err != nil {
    panic(err)
}
```

### 查询服务实例
```go
services, err := ns.Find("logic")
if err != nil {
    // 处理错误
}
for _, svc := range services {
    fmt.Printf("Found: %s at %s\n", svc.ServiceID(), svc.DialURL())
}
```

### 订阅服务变更
```go
err := ns.Subscribe("comet", func(services []kim.ServiceRegistration) {
    log.Infof("comet instances changed: %d", len(services))
    // 更新本地缓存/连接池
})
```

### gRPC 客户端使用 Consul resolver
```go
// 注册 resolver builder
resolver.Register(naming.NewConsulResolverBuilder(ns))

// 通过 consul:// scheme 拨号
conn, err := grpc.Dial(
    "consul:///logic",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
    grpc.WithDefaultServiceConfig(`{"loadBalancingPolicy":"round_robin"}`),
)
```

## 依赖关系
- `github.com/hashicorp/consul/api` - Consul 客户端
- `google.golang.org/grpc/resolver` - gRPC resolver 接口
- `github.com/klintcheng/kim/internal/kim` - ServiceRegistration 等核心接口
- `github.com/klintcheng/kim/internal/logger` - 日志模块
