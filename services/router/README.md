# Router 服务 - IP 区域路由

## 模块概述

Router 是 kim 系统的边缘 HTTP 服务，负责根据客户端 IP 进行地理位置解析，返回最优的 Gateway 接入点列表。它作为 DNS 层之后的调度服务，承担以下职责：

- **IP 地理位置解析**：基于 ip2region 库查询 IP 所属国家/地区
- **区域映射**：将国家/地区映射到对应的部署 Region
- **IDC 权重分片**：在 Region 内按权重选择 IDC 机房
- **Gateway 发现**：通过 Consul 发现对应 IDC 标签下的 Gateway 实例
- **负载均衡**：基于 token 哈希在多个 Gateway 间做一致性选择，返回 3 个候选节点

服务默认监听端口：HTTP `:8100`。

**注意**：Router 是纯 HTTP 服务，不提供 gRPC 接口，也不处理 IM 消息业务。

## 架构设计

Router 是一个轻量级 HTTP 服务，架构简洁：

```
┌─────────────────────────────────────────────────────────────┐
│                      客户端                                  │
│            GET /api/lookup/{token}                           │
└──────────────────────────┬──────────────────────────────────┘
                           │ HTTP
┌──────────────────────────▼──────────────────────────────────┐
│                    net/http Server                           │
│               ┌─────────────────────┐                        │
│               │  /health            │ 健康检查                │
│               │  /api/lookup/{token}│ 区域查询                │
│               └─────────┬───────────┘                        │
└─────────────────────────┼────────────────────────────────────┘
                          │
┌─────────────────────────▼────────────────────────────────────┐
│                      RouterApi Handler                       │
│                                                              │
│  1. 获取客户端真实 IP                                         │
│  2. Ip2region 查询国家/地区                                   │
│  3. Mapping: 国家 → Region ID                                │
│  4. Region 内权重选择 IDC                                    │
│  5. Consul 按 IDC 标签查询 Gateway 列表                      │
│  6. token 哈希选择 3 个 Gateway                               │
│  7. 返回 Gateway 域名列表                                     │
└───────────┬───────────────────────┬──────────────────────────┘
            │                       │
┌───────────▼─────────┐   ┌─────────▼───────────┐
│  ip2region.xdb      │   │  Consul             │
│  (IP 地理数据库)     │   │  (服务发现)          │
│  - 全内存加载        │   │  - Find(SNWGateway) │
└─────────────────────┘   │  - Tags 过滤 IDC     │
                          └─────────────────────┘
                          ┌─────────────────────┐
                          │  配置数据文件         │
                          │  - mapping.json      │
                          │  - regions.json      │
                          └─────────────────────┘
```

## 关键组件

### 核心结构体

```go
// Server Router HTTP 服务
type Server struct {
    config   *Config         // 服务配置
    dataPath string          // 数据文件目录
    srv      *http.Server    // HTTP Server
    naming   naming.Naming   // Consul 服务发现
    log      *logger.Logger  // 日志实例
}
```

### RouterApi - HTTP API 处理器

`services/router/handler/router.go`

```go
type RouterApi struct {
    Naming   naming.Naming       // Consul 服务发现
    IpRegion ipregion.IpRegion   // IP 地理位置查询
    Config   conf.Router         // 路由配置（Mapping + Regions）
}
```

核心方法：
- `Lookup(w, req)` - 处理 `/api/lookup/{token}` 请求，返回 JSON 格式的接入点信息

响应结构：
```go
type LookUpResp struct {
    UTC      int64    `json:"utc"`       // 服务器时间戳
    Location string   `json:"location"`  // 地理位置（国家/地区）
    Domains  []string `json:"domains"`   // 推荐 Gateway 域名列表（3个）
}
```

### 查询流程详解

1. **获取真实 IP**：通过 `kim.RealIP(req)` 从 X-Forwarded-For 等头中获取客户端真实 IP
2. **IP 地理查询**：调用 `IpRegion.Search(ip)` 返回国家/省/市/ISP 信息
3. **降级处理**：查询失败或内网 IP 默认使用 `DefaultLocation = "中国"`
4. **区域映射**：通过 `Mapping` 表将国家映射到 Region ID，未找到返回 403 Forbidden
5. **IDC 选择**：token 哈希取模 Region 的 Slots，选择权重分片对应的 IDC
6. **服务发现**：调用 Consul `Find(wire.SNWGateway, "IDC:"+idcID)` 查询该 IDC 下所有 Gateway
7. **节点选择**：token 哈希从 Gateway 列表中环形选择 3 个节点（不足则全返回）
8. **提取域名**：从 Gateway 服务 Meta 中取出 domain 字段返回

### IpRegion - IP 地理查询接口

`services/router/ipregion/ipregion.go`

```go
// IpInfo IP 地理信息
type IpInfo struct {
    Country string  // 国家
    Region  string  // 区域/省
    City    string  // 城市
    ISP     string  // 运营商
}

// IpRegion IP 查询接口
type IpRegion interface {
    Search(ip string) (*IpInfo, error)
}
```

基于 `lionsoul2014/ip2region` 实现：
- xdb 数据库文件全量加载到内存，查询性能高
- 数据格式：`国家|区域|省份|城市|ISP`
- 支持 IPv4

### 路由配置加载

`services/router/conf/router.go`

#### 数据结构

```go
// IDC 机房定义
type IDC struct {
    ID     string  // IDC 标识（如 SH_ALI）
    Weight int     // 权重
}

// Region 区域定义（含 IDC 权重分片）
type Region struct {
    ID    string   // Region ID
    Idcs  []IDC    // 该 Region 下的 IDC 列表
    Slots []byte   // 权重分片槽位
}

// Router 路由数据
type Router struct {
    Mapping map[Country]string  // 国家 → Region ID 映射
    Regions map[string]*Region  // Region ID → Region 定义
}
```

#### 配置文件

需要两个 JSON 配置文件，放在 `--data/-d` 指定的目录下（默认 `services/router/data`）：

**mapping.json** - 国家/地区到 Region 的映射：
```json
[
  {
    "region": "cn",
    "locations": ["中国", "北京", "上海", "广东"]
  },
  {
    "region": "us",
    "locations": ["美国", "United States"]
  }
]
```

**regions.json** - Region 和 IDC 的权重配置：
```json
[
  {
    "ID": "cn",
    "Idcs": [
      {"ID": "SH_ALI", "Weight": 10},
      {"ID": "BJ_TENCENT", "Weight": 10}
    ]
  }
]
```

权重分片逻辑：
- 每个 IDC 根据 Weight 在 Slots 数组中占据对应数量的槽位
- 如 Weight=10 的 IDC 占 10 个槽位
- token 哈希对 Slots 长度取模，落到哪个槽位就选哪个 IDC
- 权重越大，被选中的概率越高

### 哈希算法

选择算法统一使用 CRC32 IEEE 哈希：
```go
func hashcode(key string) int {
    hash32 := crc32.NewIEEE()
    hash32.Write([]byte(key))
    return int(hash32.Sum32())
}
```

token 一般由客户端生成（如设备 ID 或随机串），保证同一客户端总是路由到相同的 Gateway 集合。

## HTTP 接口

Router 不提供 gRPC 接口，仅提供 HTTP 接口。

### API 列表

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health` | 健康检查，返回 "ok" |
| GET | `/api/lookup/{token}` | 查询接入点，返回 Gateway 域名列表 |

### Lookup API 详情

**请求**：
```
GET /api/lookup/abc123xyz
X-Forwarded-For: 1.2.3.4
```

`{token}` 为客户端生成的唯一标识（如设备 ID、安装 ID 等），用于一致性哈希选择。

**成功响应**（200 OK）：
```json
{
  "utc": 1719388800,
  "location": "中国",
  "domains": [
    "ws://kingimcloud.com",
    "ws://sh2.kingimcloud.com",
    "ws://sh3.kingimcloud.com"
  ]
}
```

**错误响应**：
- 403 Forbidden：客户端所在国家/地区不在服务范围内
- 500 Internal Server Error：Consul 查询失败等内部错误

## 配置说明

配置文件：`services/router/conf.yaml`

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `service_id` | string | `router-1` | 服务唯一 ID |
| `listen` | string | `:8100` | HTTP 监听地址 |
| `public_address` | string | `127.0.0.1` | 服务对外公布地址 |
| `public_port` | int | `8100` | HTTP 对外端口 |
| `monitor_port` | int | `8011` | 监控端口（当前未使用，健康检查在同端口） |
| `consul_url` | string | `http://127.0.0.1:8500` | Consul 地址 |
| `log_level` | string | `info` | 日志级别 |
| `kafka.enable` | bool | `false` | 是否启用 Kafka 日志 |
| `resilience.*` | object | - | 弹性保护配置（Router 无 gRPC 出站调用，暂不生效） |
| `trace.*` | object | - | 链路追踪配置 |
| `grpc.*` | object | - | gRPC 配置（Router 不提供 gRPC 服务，暂不生效） |

### 数据文件依赖

Router 启动时需加载以下数据文件（通过 `--data/-d` 指定目录）：

| 文件 | 说明 |
|------|------|
| `ip2region.xdb` | IP 地理信息数据库 |
| `mapping.json` | 国家/地区 → Region 映射配置 |
| `regions.json` | Region → IDC 权重配置 |

## 启动方式

### 命令行启动

```bash
# 默认配置启动（默认数据目录 services/router/data）
./bin/kim router

# 指定配置文件和数据目录
./bin/kim router -c services/router/conf.yaml -d /path/to/data
```

### 命令行参数

| 参数 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/router/conf.yaml` | 配置文件路径 |
| `--data` | `-d` | `services/router/data` | 数据文件目录（含 ip2region.xdb、mapping.json、regions.json） |

### Make 命令

```bash
# 前台启动调试
make run-router-fg

# 后台启动
make run-all
```

**注意**：启动 Router 前需确保 Consul 已启动，且数据目录下的配置文件和 xdb 文件存在。

### 测试 API

```bash
# 健康检查
curl http://127.0.0.1:8100/health

# 查询接入点
curl http://127.0.0.1:8100/api/lookup/test-token-123
```

## 依赖关系

### internal 模块依赖

| 模块 | 用途 |
|------|------|
| `internal/config` | Viper 配置加载 |
| `internal/logger` | Zap 日志封装 |
| `internal/naming` | Consul 服务发现（查询 Gateway 实例） |
| `internal/trace` | OpenTelemetry 链路追踪 |
| `internal/kim` | `RealIP` 工具函数，`ServiceRegistration` 接口 |

### 第三方库依赖

| 库 | 用途 |
|----|------|
| `lionsoul2014/ip2region/binding/golang/xdb` | IP 地理位置查询（内存模式） |

### 数据文件依赖

| 文件 | 用途 |
|------|------|
| `ip2region.xdb` | IP 地理信息数据库 |
| `mapping.json` | 国家到 Region 的映射 |
| `regions.json` | Region 到 IDC 的权重配置 |

### 其他服务依赖

| 服务 | 用途 |
|------|------|
| Consul | 服务发现：查询带指定 IDC 标签的 Gateway 实例列表 |
| Gateway | 仅通过 Consul 发现，不直接调用 |

### 协议层依赖

| 模块 | 用途 |
|------|------|
| `wire` | 常量定义（服务名 SNWGateway） |

### 不依赖的模块

Router 服务职责单一，**不依赖**以下模块：
- 不依赖 MySQL/Redis（无状态，不持久化数据）
- 不依赖 gRPC 相关模块（不提供也不调用 gRPC 服务）
- 不依赖 `wire/pkt` 协议包（不处理 IM 消息）
- 不依赖其他三个业务服务（gateway/comet/logic）的客户端
