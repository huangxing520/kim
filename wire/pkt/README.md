# wire/pkt 模块 - 消息包定义与编解码

## 1. 模块概述

`wire/pkt` 模块定义了 Kim IM 系统中客户端与网关通信的两种消息包类型：`LogicPkt`（业务消息包）和 `BasicPkt`（心跳包），并提供统一的序列化/反序列化接口。所有消息包均通过魔数进行类型识别。

## 2. 协议/架构设计

### 2.1 消息包帧格式

所有消息在传输时遵循以下帧格式：

```
┌──────────────┬──────────────────────────┬──────────────────────────┐
│  魔数(4B)    │                          │                          │
│ MagicLogicPkt│   Header(Protobuf)       │      Body(bytes)         │
│ 0xc311a365   │   [uint32 len][data]     │   [uint32 len][data]     │
└──────────────┴──────────────────────────┴──────────────────────────┘

┌──────────────┬──────────────┬──────────────┬──────────────────────────┐
│  魔数(4B)    │  Code(2B)    │ Length(2B)   │         Body             │
│ MagicBasicPkt│  Ping/Pong   │              │     [Length bytes]       │
│ 0xc315a765   │              │              │                          │
└──────────────┴──────────────┴──────────────┴──────────────────────────┘
```

- **魔数**：4 字节，用于在读取时区分包类型
- **LogicPkt**：Header 使用 Protobuf 序列化，Body 为任意二进制（通常也是 Protobuf）
- **BasicPkt**：固定 4 字节头（Code + Length），用于轻量心跳

### 2.2 LogicPkt 编解码流程

**编码（Encode）**：
1. 将 `Header` 序列化为 Protobuf 字节
2. 写入 Header 长度（uint32 大端）+ Header 内容
3. 写入 Body 长度（uint32 大端）+ Body 内容

**解码（Decode）**：
1. 读取 uint32 长度前缀，读取 Header 字节并反序列化 Protobuf
2. 读取 uint32 长度前缀，读取 Body 字节

### 2.3 BasicPkt 编解码流程

**编码（Encode）**：
1. 写入 Code（uint16 大端）
2. 写入 Length（uint16 大端）
3. 写入 Body（Length 字节）

**解码（Decode）**：
1. 读取 Code（uint16）
2. 读取 Length（uint16）
3. 如果 Length > 0，读取 Length 字节的 Body

## 3. 关键组件

### 3.1 Packet 接口

所有可编解码消息包的统一接口：

```go
type Packet interface {
    Decode(r io.Reader) error
    Encode(w io.Writer) error
}
```

### 3.2 核心函数

| 函数 | 说明 |
|------|------|
| `Read(r io.Reader) (interface{}, error)` | 通用读取：先读魔数，自动识别包类型并解码 |
| `MustReadLogicPkt(r)` | 读取并断言为 `*LogicPkt` |
| `MustReadBasicPkt(r)` | 读取并断言为 `*BasicPkt` |
| `Marshal(p Packet) []byte` | 通用序列化：自动写入对应魔数 + 编码内容 |
| `FindMeta(meta []*Meta, key string)` | 在 Meta 切片中查找指定 key 的值 |

### 3.3 LogicPkt 构建选项

使用函数式选项模式构建 Header：

```go
func WithStatus(status Status) HeaderOption
func WithSeq(seq uint32) HeaderOption
func WithChannel(channelID string) HeaderOption
func WithDest(dest string) HeaderOption
```

## 4. 核心数据结构

### 4.1 LogicPkt - 业务消息包

```go
type LogicPkt struct {
    Header          // Protobuf 定义的 Header（嵌入）
    Body   []byte   `json:"body,omitempty"`
}
```

**Header 核心字段**（来自 Protobuf 定义）：
- `Command string` - 命令字（如 `"chat.user.talk"`）
- `Sequence uint32` - 消息序列号（自动生成）
- `ChannelId string` - 通道 ID
- `Status Status` - 状态码
- `Dest string` - 目标地址
- `Meta []*Meta` - 元数据键值对列表

**Meta 结构**：
```go
type Meta struct {
    Key   string
    Value string
    Type  MetaType // MetaType_string / MetaType_int / MetaType_float
}
```

**LogicPkt 主要方法**：

| 方法 | 说明 |
|------|------|
| `New(command string, options ...HeaderOption) *LogicPkt` | 创建新消息包（自动生成 Sequence） |
| `NewFrom(header *Header) *LogicPkt` | 从已有 Header 复制创建 |
| `Decode(r io.Reader) error` | 从 Reader 解码 |
| `Encode(w io.Writer) error` | 编码到 Writer |
| `ReadBody(val proto.Message) error` | 将 Body 反序列化为 Protobuf 消息 |
| `WriteBody(val proto.Message) *LogicPkt` | 将 Protobuf 消息序列化到 Body |
| `StringBody() string` | 返回 Body 的字符串形式 |
| `AddMeta(m ...*Meta)` | 添加 Meta 键值对 |
| `AddStringMeta(key, value string)` | 添加字符串类型 Meta |
| `GetMeta(key string) (interface{}, bool)` | 查找 Meta 值（自动类型转换） |
| `DelMeta(key string)` | 删除指定 key 的 Meta |
| `ServiceName() string` | 从 Command 解析服务名（如 `chat.user.talk` → `chat`） |

### 4.2 BasicPkt - 心跳包

```go
type BasicPkt struct {
    Code   uint16  // 操作码
    Length uint16  // Body 长度
    Body   []byte  // 消息体
}
```

**心跳操作码常量**：

| 常量 | 值 | 说明 |
|------|-----|------|
| `CodePing` | 1 | Ping 心跳请求 |
| `CodePong` | 2 | Pong 心跳响应 |

## 5. 使用示例

### 5.1 创建并编码 LogicPkt

```go
import (
    "bytes"
    "github.com/klintcheng/kim/wire/pkt"
)

// 创建一个聊天消息
p := pkt.New("chat.user.talk",
    pkt.WithChannel("user123"),
    pkt.WithDest("user456"),
)

// 添加 Meta 信息
p.AddStringMeta("dest.server", "wgateway")
p.AddStringMeta("dest.channels", "channel1,channel2")

// 写入 Protobuf Body（假设有 TalkRequest 消息）
// p.WriteBody(&TalkRequest{...})

// 序列化（自动添加魔数）
data := pkt.Marshal(p)
```

### 5.2 解码 LogicPkt

```go
import "github.com/klintcheng/kim/wire/pkt"

// 从连接读取并自动识别类型
packet, err := pkt.Read(reader)
if err != nil {
    // 处理错误
}

// 断言为 LogicPkt
logicPkt, ok := packet.(*pkt.LogicPkt)
if !ok {
    // 不是业务消息包
}

// 读取 Protobuf Body
// var req TalkRequest
// err = logicPkt.ReadBody(&req)

// 获取 Meta
if dest, ok := logicPkt.GetMeta("dest.server"); ok {
    fmt.Println("目标服务器:", dest)
}

// 获取服务名
serviceName := logicPkt.ServiceName() // 返回 "chat"
```

### 5.3 使用 MustRead 便捷函数

```go
// 已知是 LogicPkt 时使用
lp, err := pkt.MustReadLogicPkt(reader)
if err != nil {
    // 处理错误
}

// 已知是心跳包时使用
bp, err := pkt.MustReadBasicPkt(reader)
if err != nil {
    // 处理错误
}
if bp.Code == pkt.CodePing {
    // 收到 Ping，回复 Pong
}
```

### 5.4 心跳包编码/解码

```go
// 创建 Ping
ping := &pkt.BasicPkt{Code: pkt.CodePing}
var buf bytes.Buffer
_ = ping.Encode(&buf)

// 解码 Pong
var pong pkt.BasicPkt
_ = pong.Decode(&buf)
```

### 5.5 从 Header 转发消息

```go
// 收到消息后转发时，使用 NewFrom 复制 Header
forwardPkt := pkt.NewFrom(&receivedPkt.Header)
forwardPkt.WriteBody(newBody)
// forwardPkt.Sequence 保持原值，便于请求-响应匹配
```

## 6. 注意事项

### ⚠️ 严禁修改的内容

1. **Protobuf 生成文件**（`*.pb.go`）- 由 `wire/build.sh` 自动生成，不要手动编辑
2. **魔数** - 修改后与旧版本客户端不兼容
3. **Codec 常量**（`CodePing`/`CodePong`）- 心跳协议固定

### 编码注意事项

- 所有长度前缀使用 **uint32 大端序**（BasicPkt 的 Length 是 uint16）
- `New()` 创建消息包时自动调用 `wire.Seq.Next()` 生成序列号，如需自定义序列号使用 `WithSeq()`
- `Marshal()` 函数通过反射判断包类型并写入对应魔数，确保传入的是指针类型（`*LogicPkt` 或 `*BasicPkt`）
- Meta 中存储的值都是 string 类型，通过 `GetMeta()` 获取时会根据 Type 自动转换为 int/float/string
- `DelMeta()` 从后往前遍历删除，避免索引错乱（修复了 #18 问题）
- `ReadFrame` 读取到 `OpClose` 帧时返回 error，上层需要处理连接关闭
- 编码 Header 和 Body 时都使用 `endian.WriteBytes`（uint32 长度前缀），不要直接写入原始字节
