# wire 模块 - 客户端协议层

## 1. 模块概述

`wire` 模块是 Kim IM 系统的客户端协议层，定义了客户端与网关之间通信的标准协议格式。该模块包含协议常量定义、消息包结构、序列化工具、序列号生成、JWT Token 认证以及 gRPC 辅助函数等核心组件。

## 2. 协议/架构设计

### 协议分层

```
┌─────────────────────────────────────────────────────┐
│                   应用层业务消息                     │
│              (chat.user.talk 等 Command)            │
├─────────────────────────────────────────────────────┤
│                  LogicPkt 消息包                    │
│          Header(Protobuf) + Body + Meta             │
├─────────────────────────────────────────────────────┤
│                  BasicPkt 心跳包                    │
│              Code + Length + Body                   │
├─────────────────────────────────────────────────────┤
│                  魔数 (4 bytes)                     │
│          MagicLogicPkt / MagicBasicPkt              │
├─────────────────────────────────────────────────────┤
│               TCP/WebSocket 帧协议                  │
│         OpCode(1 byte) + Length + Payload           │
└─────────────────────────────────────────────────────┘
```

### 魔数定义

| 魔数常量 | 值 | 用途 |
|---------|-----|------|
| `MagicLogicPkt` | `{0xc3, 0x11, 0xa3, 0x65}` | 标识 LogicPkt 业务消息包 |
| `MagicBasicPkt` | `{0xc3, 0x15, 0xa7, 0x65}` | 标识 BasicPkt 心跳包 |

### 服务名常量

服务名用于 Consul 服务发现，**严禁修改**：

| 常量 | 值 | 说明 |
|-----|-----|------|
| `SNWGateway` | `"wgateway"` | WebSocket 网关服务 |
| `SNTGateway` | `"tgateway"` | TCP 网关服务 |
| `SNLogin` / `SNChat` | `"chat"` | 聊天/登录业务服务 |
| `SNService` | `"royal"` | Logic 服务（gRPC 服务端） |

### 协议类型

| 常量 | 值 |
|-----|-----|
| `ProtocolTCP` | `"tcp"` |
| `ProtocolWebsocket` | `"websocket"` |

## 3. 关键组件

### 3.1 definitions.go - 协议常量

定义所有全局协议常量，包括：
- 路由算法常量（`AlgorithmHashSlots`）
- 客户端命令字（`Command*`）
- Meta Key 常量（`MetaDestServer`、`MetaDestChannels`）
- 协议类型、服务名、魔数
- 业务参数（离线消息过期、分页大小、Token 过期等）
- 消息类型枚举（文本/图片/语音/视频）

### 3.2 seq.go - 全局序列号生成器

线程安全的原子递增序列号生成器，用于消息 Header 的 `Sequence` 字段。溢出 `math.MaxUint32` 后自动回绕到 1。

```go
var Seq = sequence{num: 1}
seq := wire.Seq.Next() // 获取下一个序列号
```

### 3.3 grpc_helper.go - gRPC 辅助函数

提供 gRPC 错误状态判断工具：

```go
func IsGrpcError(err error, code codes.Code) bool
```

### 3.4 endian/ - 大端序二进制读写

大端字节序的二进制编解码工具，支持：
- 固定宽度整数：`ReadUint8/16/32/64`、`WriteUint8/16/32/64`
- 长度前缀数据：
  - `ReadBytes`/`WriteBytes` - uint32 长度前缀
  - `ReadShortBytes`/`WriteShortBytes` - uint16 长度前缀
  - `ReadString`/`WriteString` - uint32 长度前缀字符串
  - `ReadShortString` - uint16 长度前缀字符串
- 固定长度数据：`ReadFixedBytes`

## 4. 核心数据结构

### 4.1 命令字常量

| 分类 | 常量 | 值 |
|-----|------|-----|
| **登录** | `CommandLoginSignIn` | `"login.signin"` |
| | `CommandLoginSignOut` | `"login.signout"` |
| **聊天** | `CommandChatUserTalk` | `"chat.user.talk"` |
| | `CommandChatGroupTalk` | `"chat.group.talk"` |
| | `CommandChatTalkAck` | `"chat.talk.ack"` |
| **离线消息** | `CommandOfflineIndex` | `"chat.offline.index"` |
| | `CommandOfflineContent` | `"chat.offline.content"` |
| **群管理** | `CommandGroupCreate` | `"chat.group.create"` |
| | `CommandGroupJoin` | `"chat.group.join"` |
| | `CommandGroupQuit` | `"chat.group.quit"` |
| | `CommandGroupMembers` | `"chat.group.members"` |
| | `CommandGroupDetail` | `"chat.group.detail"` |

### 4.2 Meta Key 常量

| 常量 | 值 | 用途 |
|-----|-----|------|
| `MetaDestServer` | `"dest.server"` | 消息目标网关的 ServiceName |
| `MetaDestChannels` | `"dest.channels"` | 消息目标 Channel 列表 |

### 4.3 业务参数常量

| 常量 | 值 | 说明 |
|-----|-----|------|
| `OfflineReadIndexExpiresIn` | 30 天 | 读索引缓存过期时间 |
| `OfflineSyncIndexCount` | 2000 | 单次同步消息索引数量 |
| `OfflineMessageExpiresIn` | 15 | 离线消息过期时间（天） |
| `MessageMaxCountPerPage` | 200 | 同步消息每页最大数量 |
| `AccessTokenExpiresIn` | 24 小时 | AccessToken 过期时间 |

### 4.4 消息类型枚举

| 常量 | 值 |
|-----|-----|
| `MessageTypeText` | 1 |
| `MessageTypeImage` | 2 |
| `MessageTypeVoice` | 3 |
| `MessageTypeVideo` | 4 |

## 5. 使用示例

### 5.1 生成序列号

```go
import "github.com/klintcheng/kim/wire"

seq := wire.Seq.Next()
```

### 5.2 判断 gRPC 错误

```go
import (
    "google.golang.org/grpc/codes"
    "github.com/klintcheng/kim/wire"
)

if wire.IsGrpcError(err, codes.NotFound) {
    // 处理未找到错误
}
```

### 5.3 使用 endian 进行二进制读写

```go
import "github.com/klintcheng/kim/wire/endian"

var buf bytes.Buffer

// 写入数据
_ = endian.WriteUint32(&buf, 100)
_ = endian.WriteString(&buf, "hello")
_ = endian.WriteBytes(&buf, []byte{0x01, 0x02})

// 读取数据
num, _ := endian.ReadUint32(&buf)
str, _ := endian.ReadString(&buf)
data, _ := endian.ReadBytes(&buf)
```

## 6. 注意事项

### ⚠️ 严禁修改的内容

1. **服务名常量**（`SN*`）- Consul 服务发现依赖这些常量，改名会导致服务不可用
2. **魔数常量**（`MagicLogicPkt`、`MagicBasicPkt`）- 修改后新旧客户端不兼容
3. **命令字常量**（`Command*`）- 客户端协议保持不变
4. **Proto 生成文件**：
   - `wire/proto/{common,protocol}.proto` - 需通过 `wire/build.sh` 重新生成
   - `wire/pkt/*.pb.go` - Protobuf 自动生成代码，不要手动编辑

### 其他注意事项

- 所有整数编码使用**大端字节序**（Big Endian）
- `wire.Seq` 是全局单例，线程安全，可在多 goroutine 中直接使用
- Meta Key 使用标准定义，不要自定义同名 key 避免冲突
- 子包说明：
  - `wire/pkt/` - 消息包定义和编解码（见 [pkt/README.md](pkt/README.md)）
  - `wire/token/` - JWT Token 认证（见 [token/README.md](token/README.md)）
