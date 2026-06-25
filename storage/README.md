# storage 模块

## 模块概述
基于 Redis 的会话存储实现，实现 `kim.SessionStorage` 接口，提供用户会话（Session）的增删查和用户位置（Location）查询功能。

## 架构设计
`RedisStorage` 使用 go-redis v9 客户端，通过 Pipeline 批量操作减少 RTT。会话数据使用 Protobuf 序列化存储，Location 使用自定义二进制格式（大端序长度前缀）存储以减少热路径上的内存分配。数据过期时间统一为 48 小时。Key 设计：`login:sn:{channelId}` 存储 Session，`login:loc:{account}` 或 `login:loc:{account}:{device}` 存储 Location。批量查询使用 MGET 一次性获取多个账号的位置。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `RedisStorage` | 结构体 | SessionStorage 接口的 Redis 实现 |

### Key 生成函数
| 函数 | 格式 | 说明 |
|------|------|------|
| `KeySession(channelId)` | `login:sn:{channelId}` | Session 存储 key |
| `KeyLocation(account, device)` | `login:loc:{account}` 或 `login:loc:{account}:{device}` | Location 存储 key |
| `KeyLocations(accounts...)` | 多个 `login:loc:{account}` | 批量 Location key 列表 |

### 常量
| 常量 | 值 | 说明 |
|------|-----|------|
| `LocationExpired` | 48 小时 | 会话和位置数据默认过期时间 |

## 核心接口

```go
// 创建 RedisStorage 实例
func NewRedisStorage(cli *redis.Client) kim.SessionStorage

// SessionStorage 接口实现
func (r *RedisStorage) Add(session *pkt.Session) error
func (r *RedisStorage) Delete(account string, channelId string) error
func (r *RedisStorage) Get(channelId string) (*pkt.Session, error)
func (r *RedisStorage) GetLocations(accounts ...string) ([]*kim.Location, error)
func (r *RedisStorage) GetLocation(account string, device string) (*kim.Location, error)
func (r *RedisStorage) RedisGet(key string) (string, error)
```

## 配置说明

### 存储格式
- **Session**：Protobuf 序列化 `pkt.Session`，存储为 string
- **Location**：自定义二进制格式
  - `[2字节大端 ChannelId长度][ChannelId字节][2字节大端 GateId长度][GateId字节]`
  - 相比 bytes.Buffer 预分配定长数组，减少 GC 压力

### Pipeline 批量操作
- `Add`：Pipeline SET 两个 key（Location + Session），一次 RTT
- `Delete`：Pipeline DEL 两个 key，一次 RTT

### MGET 批量查询
- `GetLocations` 使用 MGET 一次性查询所有账号的 Location，N 个账号只需一次 RTT
- 返回结果与输入 accounts 顺序一致，不在线的账号对应 nil
- 全部为 nil 时返回 `kim.ErrSessionNil`

### 错误处理
- Session 不存在（redis.Nil）返回 `kim.ErrSessionNil`
- Redis 其他错误直接返回

## 使用示例

### 创建 RedisStorage
```go
import (
    "github.com/redis/go-redis/v9"
    "github.com/klintcheng/kim/storage"
)

// 创建 Redis 客户端
rdb := redis.NewClient(&redis.Options{
    Addr: "127.0.0.1:6379",
})

// 创建会话存储
sessionStorage := storage.NewRedisStorage(rdb)
```

### 添加会话
```go
import "github.com/klintcheng/kim/wire/pkt"

session := &pkt.Session{
    ChannelId: "ch-abc123",
    Account:   "user001",
    GateId:    "gateway-1",
    App:       "kim",
    RemoteIP:  "192.168.1.100",
}
if err := sessionStorage.Add(session); err != nil {
    log.Errorf("add session: %v", err)
}
```

### 查询会话
```go
session, err := sessionStorage.Get("ch-abc123")
if err == kim.ErrSessionNil {
    // 会话不存在
} else if err != nil {
    // Redis 错误
}
```

### 批量查询用户位置
```go
locs, err := sessionStorage.GetLocations("user001", "user002", "user003")
if err != nil {
    // 处理错误
}
for i, loc := range locs {
    if loc == nil {
        log.Infof("user%d offline", i+1)
        continue
    }
    log.Infof("user%d at %s/%s", i+1, loc.GateId, loc.ChannelId)
}
```

### 删除会话
```go
if err := sessionStorage.Delete("user001", "ch-abc123"); err != nil {
    log.Errorf("delete session: %v", err)
}
```

## 依赖关系
- `github.com/redis/go-redis/v9` - Redis Go 客户端（v9 版本）
- `google.golang.org/protobuf/proto` - Protobuf 序列化
- `github.com/klintcheng/kim/internal/kim` - SessionStorage 接口、Location 结构体、ErrSessionNil
- `github.com/klintcheng/kim/wire/pkt` - Session Protobuf 消息类型
