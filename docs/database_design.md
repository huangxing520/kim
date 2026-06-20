# Kim 数据库设计文档

本文档说明 kim 项目中 MySQL 和 Redis 的使用方式，包括数据库数量、表结构、字段设计、Redis 数据结构以及初始化流程。

---

## 一、MySQL 数据库

项目使用两个 MySQL 数据库，均采用 `utf8mb4` 字符集（支持完整 Unicode，含 emoji）：

- **`kim_base`** — 基础数据（用户、群组、群成员）
- **`kim_message`** — 消息数据（消息索引、消息内容）

---

## 二、表结构设计

所有表使用 GORM 自动迁移（`AutoMigrate`）创建，遵循统一的命名规则：
- 表名前缀：`t_`（例如 `User` → `t_user`）
- 表名单数形式（`SingularTable: true`）

### 公共基字段（Model 嵌入）

每个表均包含以下公共字段（来自 `Model` 结构体）：

```go
type Model struct {
    ID        int64     `gorm:"primarykey"`   // 主键（雪花算法生成）
    CreatedAt time.Time                         // 创建时间
    UpdatedAt time.Time                         // 更新时间
}
```

---

### 2.1 t_user（用户表）

**所属数据库：** `kim_base`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | bigint | 主键 | 用户唯一 ID（雪花算法） |
| `created_at` | datetime | | 创建时间 |
| `updated_at` | datetime | | 更新时间 |
| `app` | varchar(30) | | 所属应用标识 |
| `account` | varchar(60) | **uniqueIndex** | 用户账号（唯一索引） |
| `password` | varchar(30) | | 登录密码 |
| `avatar` | varchar(200) | | 头像 URL |
| `nickname` | varchar(20) | | 用户昵称 |

**对应结构体：**

```go
type User struct {
    Model
    App      string `gorm:"size:30"`
    Account  string `gorm:"uniqueIndex;size:60"`
    Password string `gorm:"size:30"`
    Avatar   string `gorm:"size:200"`
    Nickname string `gorm:"size:20"`
}
```

---

### 2.2 t_group（群组表）

**所属数据库：** `kim_base`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | bigint | 主键 | 群组唯一 ID（雪花算法） |
| `created_at` | datetime | | 创建时间 |
| `updated_at` | datetime | | 更新时间 |
| `group` | varchar(30) | **uniqueIndex** | 群组 ID（base36 编码） |
| `app` | varchar(30) | | 所属应用标识 |
| `name` | varchar(50) | | 群组名称 |
| `owner` | varchar(60) | | 群主账号 |
| `avatar` | varchar(200) | | 群头像 URL |
| `introduction` | varchar(300) | | 群简介 |

**对应结构体：**

```go
type Group struct {
    Model
    Group        string `gorm:"uniqueIndex;size:30"`
    App          string `gorm:"size:30"`
    Name         string `gorm:"size:50"`
    Owner        string `gorm:"size:60"`
    Avatar       string `gorm:"size:200"`
    Introduction string `gorm:"size:300"`
}
```

---

### 2.3 t_group_member（群成员表）

**所属数据库：** `kim_base`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | bigint | 主键 | 记录唯一 ID（雪花算法） |
| `created_at` | datetime | | 加入时间（`join_time`） |
| `updated_at` | datetime | | 更新时间 |
| `account` | varchar(60) | **联合唯一索引** `uni_gp_acc` | 成员账号 |
| `group` | varchar(30) | **联合唯一索引** `uni_gp_acc`、普通索引 | 群组 ID |
| `alias` | varchar(30) | | 群内昵称/别名 |

**对应结构体：**

```go
type GroupMember struct {
    Model
    Account string `gorm:"uniqueIndex:uni_gp_acc;size:60"`
    Group   string `gorm:"uniqueIndex:uni_gp_acc;index;size:30"`
    Alias   string `gorm:"size:30"`
}
```

---

### 2.4 t_message_index（消息索引表）

**所属数据库：** `kim_message`

此表采用 **写扩散** 模型：每条消息为每个接收方生成一条索引记录。

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | bigint | 主键 | 索引记录唯一 ID（雪花算法） |
| `account_a` | varchar(60) | **索引**、not null | 队列唯一标识（接收方/所属方的账号） |
| `account_b` | varchar(60) | not null | 另一方账号（发送方或接收方） |
| `direction` | tinyint | default 0, not null | 方向标识：1 表示 account_a 为发送者 |
| `message_id` | bigint | not null | 关联的消息内容 ID（关联 `t_message_content.id`） |
| `group` | varchar(30) | | 群组 ID（单聊时为 null） |
| `send_time` | bigint | **索引**、not null | 消息发送时间（纳秒时间戳） |

**对应结构体：**

```go
type MessageIndex struct {
    ID        int64  `gorm:"primarykey"`
    AccountA  string `gorm:"index;size:60;not null;comment:队列唯一标识"`
    AccountB  string `gorm:"size:60;not null;comment:另一方"`
    Direction byte   `gorm:"default:0;not null;comment:1表示AccountA为发送者"`
    MessageID int64  `gorm:"not null;comment:关联消息内容表中的ID"`
    Group     string `gorm:"size:30;comment:群ID，单聊情况为空"`
    SendTime  int64  `gorm:"index;not null;comment:消息发送时间"`
}
```

---

### 2.5 t_message_content（消息内容表）

**所属数据库：** `kim_message`

| 字段 | 类型 | 约束 | 说明 |
|------|------|------|------|
| `id` | bigint | 主键 | 消息唯一 ID（雪花算法） |
| `type` | tinyint | default 0 | 消息类型（1-文本, 2-图片, 3-语音, 4-视频） |
| `body` | varchar(5000) | not null | 消息正文 |
| `extra` | varchar(500) | | 扩展字段（如图片/语音的元数据） |
| `send_time` | bigint | **索引** | 消息发送时间（纳秒时间戳） |

**对应结构体：**

```go
type MessageContent struct {
    ID       int64  `gorm:"primarykey"`
    Type     byte   `gorm:"default:0"`
    Body     string `gorm:"size:5000;not null"`
    Extra    string `gorm:"size:500"`
    SendTime int64  `gorm:"index"`
}
```

---

## 三、数据库关系图

```
kim_base (基础数据库)
├── t_user              ── 用户账号与资料
├── t_group             ── 群组信息
└── t_group_member      ── 群组成员关系（多对多）

kim_message (消息数据库)
├── t_message_index     ── 消息索引（写扩散，收件箱模型）
└── t_message_content   ── 消息内容体
```

### 核心业务关系

```
User 1 ────→ GroupMember ←──── Group
  │                                  │
  │                             发送群消息
  │                                  │
  └──→ MessageIndex              ←──┘
          │
          └──→ MessageContent (通过 message_id 关联)
```

---

## 四、MySQL 初始化流程

### 初始化函数

位于 [services/service/database/mysql.go](file:///root/program/go/kim/services/service/database/mysql.go)：

```go
func InitDb(driver string, dsn string) (*gorm.DB, error)
```

### 初始化步骤

1. **创建 GORM 日志记录器**：慢查询阈值 200ms，日志级别 Warn
2. **选择数据库驱动**：目前仅支持 MySQL（`gorm.io/driver/mysql`），已预留 SQLite 支持（注释中）
3. **配置 GORM**：
   - `TablePrefix: "t_"` — 表名前缀
   - `SingularTable: true` — 单数表名
   - `NameReplacer` — 将 `CID` 替换为 `Cid`
4. **配置连接池参数**（修复 #11）：

| 参数 | 值 | 说明 |
|------|-----|------|
| `SetMaxOpenConns` | 100 | 最大打开连接数 |
| `SetMaxIdleConns` | 20 | 最大空闲连接数 |
| `SetConnMaxLifetime` | 1 小时 | 连接最大存活时间 |
| `SetConnMaxIdleTime` | 10 分钟 | 连接最大空闲时间 |

### 服务启动时调用

位于 [services/service/server.go](file:///root/program/go/kim/services/service/server.go)：

```go
// 分两个数据库初始化
baseDb, _    = database.InitDb(config.Driver, config.BaseDb)    // kim_base
messageDb, _ = database.InitDb(config.Driver, config.MessageDb) // kim_message

// 自动建表（创建/更新表结构）
baseDb.AutoMigrate(&database.Group{}, &database.GroupMember{}, &database.User{})
messageDb.AutoMigrate(&database.MessageIndex{}, &database.MessageContent{})
```

> 首次运行时需手动创建数据库（`CREATE DATABASE kim_base; CREATE DATABASE kim_message;`），service 启动时 `AutoMigrate` 会自动创建表。

---

## 五、Redis

### 5.1 Redis 初始化

项目中有两份 Redis 初始化代码：

1. **[services/service/database/redis.go](file:///root/program/go/kim/services/service/database/redis.go)** — Service(Royal) 服务使用
2. **[storage/redis_impl.go](file:///root/program/go/kim/services/service/database/redis.go)** — 底层 Session 存储使用（实际也是调用 `services/service/database/redis.go` 的 `InitRedis`）

```go
func InitRedis(addr string, pass string) (*redis.Client, error)
```

**连接参数：**

| 参数 | 值 |
|------|-----|
| DialTimeout | 5s |
| ReadTimeout | 5s |
| WriteTimeout | 5s |

同时预留了哨兵模式支持（`InitFailoverRedis`）。

### 5.2 Redis 的三种用途

#### 用途一：Session 会话存储

位于 [storage/redis_impl.go](file:///root/program/go/kim/storage/redis_impl.go)，用于维护用户在线状态。

**数据结构：**

| Redis Key | Value | 过期时间 | 说明 |
|-----------|-------|----------|------|
| `login:sn:{channelId}` | Protobuf 序列化的 `Session` | 48h | 会话信息（channelId → Session） |
| `login:loc:{account}` | 二进制编码的 `Location` | 48h | 用户位置（account → ChannelId + GateId） |

**Session 结构体 (Protobuf)：**
- `ChannelId` — 连接通道 ID
- `GateId` — 网关服务 ID
- `Account` — 用户账号
- `Device` — 设备类型
- `Zone` — 可用区
- `Isp` — 网络运营商
- `RemoteIP` — 客户端 IP
- `Tags` — 标签

**Location 结构体：**
- `ChannelId` — 连接通道 ID
- `GateId` — 网关服务 ID

**操作接口（`SessionStorage`）：**

| 方法 | 说明 |
|------|------|
| `Add(session)` | 添加会话（Pipeline 批量写入 sn 和 loc） |
| `Delete(account, channelId)` | 删除会话（Pipeline 批量删除 sn 和 loc） |
| `Get(channelId)` | 根据 channelId 获取 Session |
| `GetLocations(accounts...)` | 批量查询多个用户的 Location（MGET） |

#### 用途二：消息已读索引

位于 [services/service/database/redis.go](file:///root/program/go/kim/services/service/database/redis.go) + [message_handler.go](file:///root/program/go/kim/services/service/handler/message_handler.go)。

**数据结构：**

| Redis Key | Value | 过期时间 | 说明 |
|-----------|-------|----------|------|
| `chat:ack:{account}` | 已确认的最大消息 ID（int64） | 30 天 | 用户已读/已确认的消息水位线 |

**用途：**
- 客户端收到消息后发送 ACK，服务端更新 Redis 中的已读索引
- 离线同步时，根据此值计算增量消息的开始时间点

#### 用途三：AccessToken 缓存

位于 [user_handler.go](file:///root/program/go/kim/services/service/handler/user_handler.go)。

**数据结构：**

| Redis Key | Value | 过期时间 | 说明 |
|-----------|-------|----------|------|
| `{account}` | JWT AccessToken 字符串 | 24h | 用户登录后生成的访问令牌 |

**流程：**
1. 用户登录成功后，验证账号密码
2. 使用 `token.Generate("secret", ...)` 生成 JWT Token
3. 将 Token 存入 Redis（key 为账号），并设置 24 小时过期
4. 返回 Token 给客户端用于后续请求鉴权

---

## 六、ID 生成器（雪花算法）

项目使用 [bwmarrin/snowflake](https://github.com/bwmarrin/snowflake) 生成分布式唯一 ID，用于所有表的主键以及消息 ID。

位于 [services/service/database/id_generator.go](file:///root/program/go/kim/services/service/database/id_generator.go)：

```go
func NewIDGenerator(nodeID int64) (*IDGenerator, error)
```

- `nodeID` 默认为本地 IP 末段数字，也可在配置中指定 `NodeID`
- 支持 `ParseBase36` 将 ID 转换为短字符串格式（用于群组 ID 展示）
- IDs 按时间递增排序，可用作消息排序依据

---

## 七、配置示例

```yaml
# services/service/conf.yaml
BaseDb: root:password@tcp(127.0.0.1:3306)/kim_base?charset=utf8mb4&parseTime=True&loc=Local
MessageDb: root:password@tcp(127.0.0.1:3306)/kim_message?charset=utf8mb4&parseTime=True&loc=Local
RedisAddrs: localhost:6379
```

---

## 八、数据库初始化 SQL

```sql
-- 创建数据库（需手动执行）
CREATE DATABASE kim_base DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE DATABASE kim_message DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- GORM AutoMigrate 自动创建以下表：
-- kim_base:    t_user, t_group, t_group_member
-- kim_message: t_message_index, t_message_content
```