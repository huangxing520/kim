# Logic 服务 - 消息持久化逻辑

## 模块概述

Logic 是 kim 系统的数据持久化服务，负责 IM 系统所有数据的存储和查询。它作为有状态的数据层，承担以下职责：

- **用户认证**：用户密码验证、JWT Token 生成
- **消息存储**：单聊/群聊消息内容存储、收件箱索引写入（扩散写模型）
- **消息同步**：离线消息索引查询、消息内容批量拉取
- **消息已读**：用户消息已读位置记录
- **群组管理**：群组 CRUD、群成员管理
- **分布式 ID**：基于 Snowflake 算法生成全局唯一消息 ID/群组 ID

服务默认监听端口：gRPC `:9002`，Monitor HTTP `:8009`。

**注意**：Logic 服务在 Consul 中注册的服务名为 `royal`（常量 `wire.SNService`）。

## 架构设计

Logic 服务是纯 gRPC 服务，采用 Handler + Data 分层架构，使用双库分离设计：

```
┌─────────────────────────────────────────────────────────────┐
│                     Comet 服务                               │
│              gRPC: LogicService.*                            │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                     gRPC Server                              │
│          ┌─────────────────────────────┐                     │
│          │      ServiceHandler         │                     │
│          │  (实现 LogicServiceServer)   │                     │
│          └──────────────┬──────────────┘                     │
└─────────────────────────┼────────────────────────────────────┘
                          │
        ┌─────────────────┼─────────────────┐
        │                 │                 │
┌───────▼───────┐ ┌───────▼───────┐ ┌───────▼───────┐
│  UserHandler  │ │MessageHandler │ │ GroupHandler  │
│ - Login()     │ │-InsertUserMsg │ │-GroupCreate   │
│               │ │-InsertGroupMsg│ │-GroupJoin     │
│               │ │-AckMessage    │ │-GroupQuit     │
│               │ │-GetOfflineIdx │ │-GroupMembers  │
│               │ │-GetOfflineCnt │ │-GroupGet      │
└───────┬───────┘ └───────┬───────┘ └───────┬───────┘
        │                 │                 │
        └─────────────────┼─────────────────┘
                          │
        ┌─────────────────┴─────────────────┐
        │                                   │
┌───────▼────────┐                 ┌────────▼───────┐
│   BaseDb       │                 │  MessageDb     │
│  (kim 库)      │                 │ (kim_message)  │
│ - User 表      │                 │ - MessageIndex │
│ - Group 表     │                 │ - MessageContent│
│ - GroupMember  │                 │                │
└───────┬────────┘                 └────────┬───────┘
        │                                   │
        └─────────────────┬─────────────────┘
                          │
                ┌─────────▼─────────┐
                │     Redis         │
                │ - AccessToken     │
                │ - MessageAck 位置  │
                └───────────────────┘
                ┌───────────────────┐
                │   IDGenerator     │
                │ (Snowflake 算法)  │
                │ - NodeID 隔离     │
                └───────────────────┘
```

### 数据库设计

采用**双库分离**设计：
- **kim 库（BaseDb）**：存储用户、群组等基础数据
- **kim_message 库（MessageDb）**：存储消息索引和消息内容

消息存储采用**收件箱模型（扩散写）**：
1. 一条消息只在 `MessageContent` 表存一份内容
2. 对每个接收者（单聊 2 条，群聊 N 条）在 `MessageIndex` 表写一条索引
3. 索引表通过 `account_a` 作为用户收件箱的分区键

## 关键组件

### 核心结构体

```go
// Server Logic gRPC 服务
type Server struct {
    config        *Config              // 服务配置
    grpcSrv       *server.GRPCServer   // gRPC Server
    naming        naming.Naming        // Consul 服务发现
    baseDb        *gorm.DB             // 基础库（用户/群组）
    messageDb     *gorm.DB             // 消息库（索引/内容）
    log           *logger.Logger       // 日志实例
    traceShutdown func()               // 链路追踪关闭函数
}
```

### ServiceHandler - gRPC 服务实现

`services/logic/handler/message_handler.go`

```go
// ServiceHandler Logic 服务核心 Handler，实现 LogicServiceServer 接口
type ServiceHandler struct {
    rpc.UnimplementedLogicServiceServer
    BaseDb    *gorm.DB             // 基础库
    MessageDb *gorm.DB             // 消息库
    Cache     *redis.Client        // Redis 缓存
    Idgen     *data.IDGenerator    // 分布式 ID 生成器
}
```

所有 gRPC 方法均为 `ServiceHandler` 的方法，分属三个业务文件：

### 用户相关（user_handler.go）

| 方法 | 说明 |
|------|------|
| `Login` | 用户登录验证：查询用户 → bcrypt 密码校验 → 生成 JWT Token → 存入 Redis |

登录流程：
1. 根据 account 查询 User 表
2. 使用 bcrypt 比对密码哈希
3. 用 AppSecret 签名生成 JWT Token
4. Token 写入 Redis（过期时间 `wire.AccessTokenExpiresIn`）
5. 返回 AccessToken

### 消息相关（message_handler.go）

| 方法 | 说明 |
|------|------|
| `InsertUserMessage` | 插入单聊消息（扩散写，双方各写一条索引） |
| `InsertGroupMessage` | 插入群聊消息（扩散写，每个成员一条索引，批量写入） |
| `AckMessage` | 记录消息已读位置到 Redis |
| `GetOfflineMessageIndex` | 查询离线消息索引列表 |
| `GetOfflineMessageContent` | 按 messageId 批量查询消息内容 |

**单聊消息写入流程**：
1. Snowflake 生成 messageId
2. 写入 MessageContent 表（消息体）
3. 事务内写入两条 MessageIndex：
   - 接收方视角：Direction=0（收到的消息）
   - 发送方视角：Direction=1（发送的消息）

**群聊消息写入流程**：
1. Snowflake 生成 messageId
2. 写入 MessageContent 表
3. 查询群所有成员
4. 按每批 1000 个成员批量写入：
   - 发送方 Direction=1，其他成员 Direction=0
   - 每批 500 条批量插入

**离线消息同步流程**：
1. 若客户端传了 messageId，查询该消息的 sendTime 作为起点
2. 若没传或消息不存在，从 Redis 取该用户已读位置
3. 默认只同步最近 `OfflineMessageExpiresIn` 天的消息
4. 查询该用户 account_a 的 MessageIndex，按时间升序，最多 `OfflineSyncIndexCount` 条
5. 更新已读位置到 Redis
6. 客户端拿到索引列表后，再批量拉取消息内容

### 群组相关（group_handler.go）

| 方法 | 说明 |
|------|------|
| `GroupCreate` | 创建群组：事务写 Group 表 + GroupMember 表（初始成员） |
| `GroupJoin` | 加入群组：插入 GroupMember 记录 |
| `GroupQuit` | 退出群组：删除 GroupMember 记录 |
| `GroupMembers` | 查询群成员列表（按加入时间排序） |
| `GroupGet` | 查询群组详情（需要 Base36 解析 groupId） |

群组 ID 使用 Snowflake 生成后转 Base36 编码，对外暴露字符串 ID。

### 数据模型（data/model.go）

GORM 模型，表名前缀为 `t_`，使用单数表名：

```go
// Model 基础模型（ID、创建/更新时间）
type Model struct {
    ID        int64 `gorm:"primarykey"`
    CreatedAt time.Time
    UpdatedAt time.Time
}

// MessageIndex 消息索引表（收件箱）
type MessageIndex struct {
    ID        int64
    AccountA  string  // 收件人 account（分区键，有索引）
    AccountB  string  // 另一方
    Direction byte    // 1 表示 AccountA 是发送者
    MessageID int64   // 关联消息内容 ID
    Group     string  // 群 ID，单聊为空
    SendTime  int64   // 发送时间（有索引）
}

// MessageContent 消息内容表
type MessageContent struct {
    ID       int64
    Type     byte
    Body     string  // 消息内容，最大 5000 字符
    Extra    string  // 扩展字段，最大 500 字符
    SendTime int64
}

// User 用户表
type User struct {
    Model
    App      string
    Account  string `gorm:"uniqueIndex"`
    Password string  // bcrypt 哈希
    Avatar   string
    Nickname string
}

// Group 群组表
type Group struct {
    Model
    Group        string `gorm:"uniqueIndex"`  // Base36 编码的群 ID
    App          string
    Name         string
    Owner        string
    Avatar       string
    Introduction string
}

// GroupMember 群成员表（联合唯一索引 uni_gp_acc）
type GroupMember struct {
    Model
    Account string `gorm:"uniqueIndex:uni_gp_acc"`
    Group   string `gorm:"uniqueIndex:uni_gp_acc;index"`
    Alias   string
}
```

### IDGenerator - 分布式 ID 生成器

`services/logic/data/id_generator.go`

基于 `bwmarrin/snowflake` 库封装：

```go
type IDGenerator struct {
    node *snowflake.Node
}
```

- NodeID 默认通过 `HashCode(service_id) % 1000` 生成，也可在配置中显式指定
- 多实例部署时需保证 NodeID 不同，避免 ID 冲突
- 支持生成 int64 ID，也支持 Base36 编码（用于群组 ID）

### 数据库连接（data/mysql.go）

GORM 初始化配置：
- 表名前缀 `t_`，单数表名
- 慢查询阈值 200ms，Warn 级别日志
- 连接池配置：最大 100 连接，最大空闲 20，连接最大存活 1 小时，最大空闲 10 分钟
- 启动时自动执行 AutoMigrate

## gRPC 接口

### 提供的 RPC 服务

Logic 作为 gRPC Server，提供 `LogicService` 供 Comet 调用：

| 分类 | 方法 | 说明 |
|------|------|------|
| **用户** | `Login` | 用户登录验证，返回 AccessToken |
| **消息** | `InsertUserMessage` | 插入单聊消息（扩散写） |
| **消息** | `InsertGroupMessage` | 插入群聊消息（扩散写，批量） |
| **消息** | `AckMessage` | 消息已读确认 |
| **消息** | `GetOfflineMessageIndex` | 查询离线消息索引列表 |
| **消息** | `GetOfflineMessageContent` | 批量查询离线消息内容 |
| **群组** | `GroupCreate` | 创建群组 |
| **群组** | `GroupJoin` | 加入群组 |
| **群组** | `GroupQuit` | 退出群组 |
| **群组** | `GroupMembers` | 查询群成员列表 |
| **群组** | `GroupGet` | 查询群详情 |

### 调用的下游服务

Logic 是数据层服务，不调用其他业务服务，仅依赖基础设施：
- MySQL：数据持久化（GORM）
- Redis：Token 缓存、已读位置
- Consul：服务注册（Logic 不主动发现其他服务）

## 配置说明

配置文件：`services/logic/conf.yaml`

| 配置项 | 类型 | 默认值 | 说明 |
|--------|------|--------|------|
| `service_id` | string | `logic-1` | 服务唯一 ID |
| `node_id` | int | `0` | Snowflake NodeID（0 表示自动由 service_id 哈希生成） |
| `listen` | string | `:9002` | gRPC 监听地址 |
| `public_address` | string | `127.0.0.1` | 服务对外公布地址 |
| `public_port` | int | `9002` | gRPC 对外端口 |
| `monitor_port` | int | `8009` | Prometheus/Health 监控端口 |
| `tags` | []string | `[]` | Consul 服务标签 |
| `consul_url` | string | `http://127.0.0.1:8500` | Consul 地址 |
| `redis_addrs` | string | `127.0.0.1:6379` | Redis 地址 |
| `redis_password` | string | - | Redis 密码 |
| `driver` | string | `mysql` | 数据库驱动（当前仅支持 mysql） |
| `base_db` | string | - | 基础库 DSN（kim 库） |
| `message_db` | string | - | 消息库 DSN（kim_message 库） |
| `log_level` | string | `info` | 日志级别 |
| `app_secret` | string | - | **必填** JWT 签名密钥 |
| `kafka.enable` | bool | `false` | 是否启用 Kafka 日志 |
| `resilience.*` | object | - | 弹性保护配置 |
| `trace.*` | object | - | 链路追踪配置 |
| `grpc.*` | object | - | gRPC TLS/认证配置 |

### 数据库初始化

需要预先创建两个数据库：

```sql
CREATE DATABASE kim_base DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE kim_message DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
```

表结构由 GORM AutoMigrate 自动创建，无需手动建表。

DSN 格式示例：
```
root:root@tcp(127.0.0.1:3306)/kim?parseTime=true
```

## 启动方式

### 命令行启动

```bash
# 默认配置启动
./bin/kim logic

# 指定配置文件
./bin/kim logic -c services/logic/conf.yaml
```

### 命令行参数

| 参数 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/logic/conf.yaml` | 配置文件路径 |

### Make 命令

```bash
# 前台启动调试
make run-logic-fg

# 后台启动
make run-all
```

**注意**：启动 Logic 前需确保 MySQL 和 Redis 已启动，且两个数据库已创建。

## 依赖关系

### internal 模块依赖

| 模块 | 用途 |
|------|------|
| `internal/config` | Viper 配置加载 |
| `internal/logger` | Zap 日志封装 |
| `internal/naming` | Consul 服务注册 |
| `internal/server` | gRPC Server 封装 |
| `internal/client` | Sentinel 初始化（弹性保护） |
| `internal/trace` | OpenTelemetry 链路追踪 |

### 第三方库依赖

| 库 | 用途 |
|----|------|
| `gorm.io/gorm` | ORM 框架 |
| `gorm.io/driver/mysql` | MySQL 驱动 |
| `redis/go-redis/v9` | Redis 客户端 |
| `bwmarrin/snowflake` | Snowflake ID 生成 |
| `golang.org/x/crypto/bcrypt` | 密码哈希 |

### 存储依赖

| 存储 | 用途 |
|------|------|
| MySQL (kim 库) | 用户表、群组表、群成员表 |
| MySQL (kim_message 库) | 消息内容表、消息索引表 |
| Redis | AccessToken 缓存、消息已读位置 |

### 其他服务依赖

| 服务 | 用途 |
|------|------|
| Consul | 服务注册（Logic 不主动发现其他服务） |

### 协议层依赖

| 模块 | 用途 |
|------|------|
| `wire/token` | JWT Token 生成 |
| `wire` | 常量定义（Token 过期时间等） |
| `gen/rpc` | gRPC Protobuf 生成代码 |
