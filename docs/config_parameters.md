# Kim 服务配置参数说明

本文档说明 kim 项目中各个服务配置文件（`conf.yaml`）的参数含义。

## 目录

- [Gateway 服务（网关）](#gateway-服务网关)
- [Server 服务（聊天服务器）](#server-服务聊天服务器)
- [Service 服务（业务服务）](#service-服务业务服务)
- [Router 服务（路由服务）](#router-服务路由服务)
- [通用参数说明](#通用参数说明)

---

## Gateway 服务（网关）

配置文件：`services/gateway/conf.yaml`（以及 `conf2.yaml` 作为多实例示例）

```yaml
ServiceID: gate01
ServiceName: wgateway
Listen: ":8000"
MonitorPort: 8001
PublicPort: 8000
PublicAddress: localhost
Tags:
  - IDC:SH_ALI
Domain: ws://kingimcloud.com
ConsulURL: 192.168.31.77:8500
AppSecret: ""
MessageGPool: 5000
ConnectionGPool: 15000
```

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ServiceID` | string | `gate_<本地IP>` | 服务实例唯一标识。若为空，启动时会自动以 `gate_` + 本地 IP（去除小数点）生成。 |
| `ServiceName` | string | `wgateway` | 服务名称，用于服务注册与发现时的服务类型标识。 |
| `Listen` | string | `:8000` | 网关监听地址（host:port 格式），客户端 WebSocket 连接此端口。 |
| `MonitorPort` | int | `8001` | 监控端口，用于 Prometheus 等监控指标暴露。 |
| `PublicPort` | int | `8000` | 对外暴露的端口号，用于服务注册时告知外部访问端口。 |
| `PublicAddress` | string | 本地 IP | 对外暴露的地址，用于服务注册时告知外部访问地址。若为空则使用本地 IP。 |
| `Tags` | []string | - | 服务标签列表，用于服务发现时的元数据筛选（如机房位置 `IDC:SH_ALI`）。 |
| `Domain` | string | - | WebSocket 域名，客户端连接网关使用的 ws/wss 地址。 |
| `ConsulURL` | string | - | Consul 服务注册中心地址（host:port 格式）。 |
| `AppSecret` | string | - | 应用密钥，用于客户端握手鉴权。 |
| `MessageGPool` | int | `10000` | 消息处理协程池大小，控制并发处理消息的 goroutine 数量。 |
| `ConnectionGPool` | int | `15000` | 连接处理协程池大小，控制并发处理连接的 goroutine 数量。 |

---

## Server 服务（聊天服务器）

配置文件：`services/server/conf.yaml`

```yaml
ServiceID: chat01
Listen: ":8005"
MonitorPort: 8006
PublicPort: 8005
PublicAddress: localhost
Tags:
  - server
Zone: zone_ali_03
ConsulURL: localhost:8500
RedisAddrs: localhost:6379
RoyalURL: http://localhost:8080
MessageGPool: 5000
ConnectionGPool: 500
```

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ServiceID` | string | `server_<本地IP>` | 服务实例唯一标识。若为空，启动时会自动以 `server_` + 本地 IP（去除小数点）生成。 |
| `Listen` | string | `:8005` | 聊天服务监听地址（host:port 格式），网关通过此端口与聊天服务通信。 |
| `MonitorPort` | int | `8006` | 监控端口，用于 Prometheus 等监控指标暴露。 |
| `PublicPort` | int | `8005` | 对外暴露的端口号，用于服务注册时告知外部访问端口。 |
| `PublicAddress` | string | 本地 IP | 对外暴露的地址，用于服务注册时告知外部访问地址。若为空则使用本地 IP。 |
| `Tags` | []string | - | 服务标签列表，用于服务发现时的元数据筛选。 |
| `Zone` | string | `zone_ali_03` | 可用区标识，用于路由服务按区域调度。 |
| `ConsulURL` | string | - | Consul 服务注册中心地址（host:port 格式）。 |
| `RedisAddrs` | string | - | Redis 地址（host:port 格式），用于缓存和会话存储。 |
| `RoyalURL` | string | - | Royal 业务服务 HTTP 地址，用于调用业务接口（如用户信息查询）。 |
| `LogLevel` | string | `DEBUG` | 日志级别（DEBUG/INFO/WARN/ERROR）。 |
| `MessageGPool` | int | `5000` | 消息处理协程池大小，控制并发处理消息的 goroutine 数量。 |
| `ConnectionGPool` | int | `500` | 连接处理协程池大小，控制并发处理连接的 goroutine 数量。 |

---

## Service 服务（业务服务）

配置文件：`services/service/conf.yaml`

```yaml
ServiceID: royal01
Listen: ":8080"
PublicPort: 8080
PublicAddress: localhost
Tags:
  - royal
ConsulURL: localhost:8500
RedisAddrs: localhost:6379
BaseDb: root:85462316@tcp(116.198.203.182:3306)/kim_base?charset=utf8mb4&parseTime=True&loc=Local
MessageDb: root:85462316@tcp(116.198.203.182:3306)/kim_message?charset=utf8mb4&parseTime=True&loc=Local
```

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ServiceID` | string | `royal_<本地IP>` | 服务实例唯一标识。若为空，启动时会自动以 `royal_` + 本地 IP（去除小数点）生成。 |
| `NodeID` | int64 | 本地 IP 末段 | 节点 ID，用于雪花算法等分布式 ID 生成。若为空则使用本地 IP 末段数字。 |
| `Listen` | string | `:8080` | 业务服务 HTTP 监听地址（host:port 格式）。 |
| `PublicPort` | int | `8080` | 对外暴露的端口号，用于服务注册时告知外部访问端口。 |
| `PublicAddress` | string | 本地 IP | 对外暴露的地址，用于服务注册时告知外部访问地址。若为空则使用本地 IP。 |
| `Tags` | []string | - | 服务标签列表，用于服务发现时的元数据筛选。 |
| `ConsulURL` | string | - | Consul 服务注册中心地址（host:port 格式）。 |
| `RedisAddrs` | string | - | Redis 地址（host:port 格式），用于缓存。 |
| `Driver` | string | `mysql` | 数据库驱动类型。 |
| `BaseDb` | string | - | 基础数据库 DSN 连接串（用户信息、账号等基础数据）。 |
| `MessageDb` | string | - | 消息数据库 DSN 连接串（消息持久化存储）。 |
| `LogLevel` | string | `INFO` | 日志级别（DEBUG/INFO/WARN/ERROR）。 |

### 数据库 DSN 格式说明

`BaseDb` 和 `MessageDb` 使用 Go MySQL 驱动的 DSN 格式：

```
用户名:密码@tcp(主机:端口)/数据库名?charset=utf8mb4&parseTime=True&loc=Local
```

- `charset=utf8mb4`：字符集，支持完整 Unicode（包括 emoji）。
- `parseTime=True`：自动将 MySQL 时间类型转换为 Go 的 `time.Time`。
- `loc=Local`：使用本地时区解析时间。

---

## Router 服务（路由服务）

配置文件：无 `conf.yaml`，配置通过代码默认值或环境变量注入。

配置结构定义在 `services/router/conf/config.go`：

```go
type Config struct {
    Listen    string `default:":8100"`
    ConsulURL string `default:"localhost:8500"`
    LogLevel  string `default:"INFO"`
}
```

| 参数 | 类型 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `Listen` | string | `:8100` | 路由服务 HTTP 监听地址（host:port 格式），客户端通过此端口查询最优网关。 |
| `ConsulURL` | string | `localhost:8500` | Consul 服务注册中心地址（host:port 格式），用于查询已注册的网关列表。 |
| `LogLevel` | string | `INFO` | 日志级别（DEBUG/INFO/WARN/ERROR）。 |

### 路由数据文件

路由服务还依赖以下数据文件（位于 `services/router/data/`）：

| 文件 | 说明 |
| --- | --- |
| `mapping.json` | 国家/地区到 Region 的映射关系。 |
| `regions.json` | Region 定义，包含 IDC 列表及权重（用于按权重分片调度）。 |
| `ip2region.db` | IP 到地区的离线查询数据库。 |

---

## 通用参数说明

以下参数在多个服务中含义一致：

### 服务标识类

- **`ServiceID`**：服务实例唯一标识。所有服务在为空时都会基于本地 IP 自动生成，前缀分别为 `gate_`/`server_`/`royal_`。
- **`ServiceName`**：服务类型名称（仅 Gateway 使用，默认 `wgateway`）。
- **`Tags`**：服务标签，用于服务发现时筛选，常见标签如 `IDC:SH_ALI`（上海阿里机房）、`IDC:HZ_ALI`（杭州阿里机房）。

### 网络监听类

- **`Listen`**：服务实际监听地址，格式 `:port` 或 `host:port`。
- **`PublicAddress`** / **`PublicPort`**：对外暴露的地址和端口，用于服务注册。当服务运行在容器或 NAT 网络后，需与 `Listen` 区分。
- **`MonitorPort`**：监控指标暴露端口（仅 Gateway 和 Server 使用）。

### 服务发现类

- **`ConsulURL`**：Consul 注册中心地址，所有服务启动时向其注册，并从中发现其他服务。

### 协程池类（仅 Gateway 和 Server）

- **`MessageGPool`**：消息处理协程池大小。控制并发处理消息的 goroutine 上限，影响消息吞吐量。
- **`ConnectionGPool`**：连接处理协程池大小。控制并发处理连接事件的 goroutine 上限，影响连接建立/断开处理能力。

> Gateway 的 `ConnectionGPool`（默认 15000）远大于 Server（默认 500），因为网关需要维持大量客户端 WebSocket 长连接，而聊天服务主要处理内部消息转发。

### 存储类

- **`RedisAddrs`**：Redis 地址，用于缓存、会话、分布式锁等。
- **`BaseDb`** / **`MessageDb`**：MySQL 数据库连接串（仅 Service 使用）。

### 其他

- **`Domain`**：WebSocket 域名（仅 Gateway 使用）。
- **`AppSecret`**：应用密钥，用于客户端鉴权（仅 Gateway 使用）。
- **`Zone`**：可用区标识，用于路由调度（仅 Server 使用）。
- **`RoyalURL`**：业务服务 HTTP 地址，供 Server 调用业务接口（仅 Server 使用）。
- **`LogLevel`**：日志级别，可选 `DEBUG`/`INFO`/`WARN`/`ERROR`。

---

## 配置加载机制

所有服务配置遵循统一的加载流程（见各服务 `conf/config.go` 的 `Init` 函数）：

1. **环境变量**：通过 `envconfig.Process("kim", &config)` 读取以 `KIM_` 为前缀的环境变量，可覆盖任意配置项。
2. **配置文件**：通过 `viper.ReadInConfig()` 读取 `conf.yaml`，配置文件优先级高于环境变量默认值。
3. **默认值**：结构体 tag 中的 `default:"xxx"` 提供兜底默认值。
4. **自动推导**：`ServiceID` 和 `PublicAddress` 为空时基于本地 IP 自动生成。

> 配置文件查找路径：当前目录 → `/etc/conf`。
