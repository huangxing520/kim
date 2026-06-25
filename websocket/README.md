# websocket 模块 - WebSocket 服务端/客户端实现

## 1. 模块概述

`websocket` 模块基于 `gobwas/ws` 库实现了 WebSocket 协议的服务端和客户端，提供与 `internal/kim` 核心接口兼容的网络层实现。该模块封装了 WebSocket 握手升级、帧编解码、读写缓冲、心跳保活、Mask 处理等功能，支持浏览器和移动端 WebSocket 接入。

## 2. 协议/架构设计

### 2.1 WebSocket 帧格式（RFC 6455）

WebSocket 帧遵循标准 RFC 6455 规范：

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-------+-+-------------+-------------------------------+
|F|R|R|R| opcode|M| Payload len |    Extended payload length    |
|I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
|N|V|V|V|       |S|             |   (if payload len==126/127)   |
| |1|2|3|       |K|             |                               |
+-+-+-+-+-------+-+-------------+ - - - - - - - - - - - - - - - +
|     Extended payload length continued, if payload len == 127  |
+ - - - - - - - - - - - - - - - +-------------------------------+
|                               |Masking-key, if MASK set to 1  |
+-------------------------------+-------------------------------+
| Masking-key (continued)       |          Payload Data         |
+-------------------------------- - - - - - - - - - - - - - - - +
:                     Payload Data continued ...                :
+ - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - - +
|                     Payload Data continued ...                |
+---------------------------------------------------------------+
```

**OpCode 定义**（与 `internal/kim` 对齐）：
- `0x0` - Continuation（续帧）
- `0x1` - Text（文本帧）
- `0x2` - Binary（二进制帧，传输 LogicPkt/BasicPkt）
- `0x8` - Connection Close
- `0x9` - Ping
- `0xA` - Pong

**关键差异**：
- 客户端发送的帧**必须设置 Mask 位**（本模块使用 `wsutil.WriteClientMessage` 自动处理）
- 服务端发送的帧**不能设置 Mask 位**（`wsutil.WriteServerMessage` 处理）
- GetPayload() 时自动检测并解密 Masked 数据

### 2.2 架构分层

```
┌─────────────────────────────────────────────────────────┐
│              业务层 (Acceptor/MessageListener)           │
├─────────────────────────────────────────────────────────┤
│              internal/kim 核心框架 (Server/Channel)      │
├─────────────────────────────────────────────────────────┤
│                 websocket 模块 (本模块)                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ ws.Server   │  │ ws.Client   │  │ ws.Upgrader     │ │
│  │ (工厂)      │  │             │  │ (HTTP→WS 升级)  │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
│  ┌─────────────────────────────────────────────────────┐│
│  │                     ws.WsConn                        ││
│  │  (bufio 读写缓冲 + gobwas/ws Frame + Flush 优化)     ││
│  └─────────────────────────────────────────────────────┘│
├─────────────────────────────────────────────────────────┤
│                  gobwas/ws (WebSocket 协议库)            │
├─────────────────────────────────────────────────────────┤
│                   net.Conn (TCP 连接)                    │
└─────────────────────────────────────────────────────────┘
```

### 2.3 读写缓冲优化（修复 #15、#16）

- 读缓冲区：4096 字节
- 写缓冲区：8192 字节（修复 #16，从 1024 扩大）
- **关键修复**（#15）：`WriteFrame` 改为写入 `bufio.Writer` 而非直接写入 `net.Conn`，由 writeloop 统一 Flush，大幅减少系统调用次数

### 2.4 升级握手流程

WebSocket 需要 HTTP Upgrade 握手：

```
客户端                          服务端
  │                               │
  │  GET /ws HTTP/1.1             │
  │  Upgrade: websocket           │
  │  Connection: Upgrade          │
  │  Sec-WebSocket-Key: ...       │
  │ ─────────────────────────────>│
  │                               │ ws.Upgrade() 执行握手
  │  HTTP/1.1 101 Switching       │
  │  Protocols                    │
  │  Upgrade: websocket           │
  │  Connection: Upgrade          │
  │  Sec-WebSocket-Accept: ...    │
  │ <─────────────────────────────│
  │                               │
  │     双向 WebSocket 帧通信      │
  │ <────────────────────────────>│
```

### 2.5 心跳机制

- 客户端配置 `Heartbeat` 后自动启动 `heartbeatloop`
- Ping 帧发送时自动设置写超时
- 读取时自动设置读超时，超时返回错误触发重连
- 服务端 Pong 响应由 gobwas/ws 自动处理

## 3. 关键组件

### 3.1 Upgrader - WebSocket 协议升级器

执行 HTTP Upgrade 握手，将 HTTP 连接升级为 WebSocket 连接。

```go
type Upgrader struct{}

func (u *Upgrader) Name() string                                    // 返回 "websocket.Server"
func (u *Upgrader) Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (kim.Conn, error)
```

Upgrade 内部调用 `ws.Upgrade(rawconn)` 完成握手，之后包装为 `WsConn`。

### 3.2 WsConn - WebSocket 连接封装

```go
type WsConn struct {
    net.Conn               // 嵌入原始 net.Conn
    rd *bufio.Reader       // 读缓冲
    wr *bufio.Writer       // 写缓冲（修复 #15：写入这里而非直连）
}
```

主要方法：
- `ReadFrame() (kim.Frame, error)` - 从 bufio.Reader 读取 WebSocket 帧
- `WriteFrame(code kim.OpCode, payload []byte) error` - 写入服务端消息到 bufio.Writer（无 Mask）
- `Flush() error` - 刷新写缓冲区

### 3.3 Frame - WebSocket 帧包装

```go
type Frame struct {
    raw ws.Frame            // 包装 gobwas/ws.Frame
}
```

实现 `kim.Frame` 接口：
- `SetOpCode(code kim.OpCode)` - 设置 OpCode
- `GetOpCode() kim.OpCode` - 获取 OpCode
- `SetPayload(payload []byte)` - 设置 Payload
- `GetPayload() []byte` - 获取 Payload（**自动处理 Mask 解密**，并清除 Mask 标记）

### 3.4 Client - WebSocket 客户端

```go
type Client struct {
    sync.Mutex
    kim.Dialer             // 拨号&握手器（由业务层设置）
    once    sync.Once      // Close 只执行一次
    id      string
    name    string
    conn    net.Conn       // 注意：这里是 net.Conn 而非 kim.Conn
    state   int32          // CAS 原子状态
    options ClientOptions
    Meta    map[string]string
}
```

主要方法：
- `Connect(addr string) error` - 解析 URL、拨号握手、启动心跳循环
- `Send(payload []byte) error` - 发送客户端二进制消息（自动 Mask，设置写超时）
- `Read() (kim.Frame, error)` - 读取一帧，自动检测 Close 帧
- `Close()` - 优雅关闭：发送 Close 帧 → 关闭连接
- `SetDialer(dialer kim.Dialer)` - 设置握手拨号器
- `ServiceID() / ServiceName() / GetMeta()` - 实现 `kim.Service` 接口

**与 TCP Client 的差异**：
- Send 使用 `wsutil.WriteClientMessage`（自动添加 Mask）
- ping 使用 `wsutil.WriteClientMessage(conn, ws.OpPing, nil)`
- Connect 时先解析 URL 验证地址格式
- conn 是 `net.Conn` 而非 `kim.Conn`（因为客户端使用 wsutil 直接读写）

### 3.5 ClientOptions - 客户端配置

```go
type ClientOptions struct {
    Heartbeat time.Duration  // 心跳间隔
    ReadWait  time.Duration  // 读超时
    WriteWait time.Duration  // 写超时
}
```

默认值同 TCP 模块：
- `ReadWait` → `kim.DefaultReadWait`（3 分钟）
- `WriteWait` → `kim.DefaultWriteWait`（10 秒）

### 3.6 Server 工厂函数

```go
func NewServer(listen string, service kim.ServiceRegistration, options ...kim.ServerOption) kim.Server
```

创建 WebSocket 协议的 Server 实例，返回 `kim.Server` 接口。内部使用 `ws.Upgrader`。

## 4. 核心数据结构

### 4.1 OpCode 定义（来自 internal/kim）

与 TCP 模块共享相同的 OpCode：

| OpCode | 值 | 说明 |
|--------|-----|------|
| `OpContinuation` | 0x0 | 续帧 |
| `OpText` | 0x1 | 文本帧 |
| `OpBinary` | 0x2 | 二进制帧（业务数据） |
| `OpClose` | 0x8 | 关闭连接 |
| `OpPing` | 0x9 | Ping 心跳 |
| `OpPong` | 0xa | Pong 心跳响应 |

### 4.2 默认超时参数（来自 internal/kim）

| 参数 | 值 | 说明 |
|------|-----|------|
| `DefaultReadWait` | 3 分钟 | 读超时 |
| `DefaultWriteWait` | 10 秒 | 写超时 |
| `DefaultLoginWait` | 10 秒 | 登录超时 |
| `DefaultHeartbeat` | 55 秒 | 默认心跳间隔 |

## 5. 使用示例

### 5.1 创建 WebSocket 服务端

```go
import (
    "github.com/klintcheng/kim/websocket"
    kim "github.com/klintcheng/kim/internal/kim"
)

// 实现业务服务
type MyService struct{}

// 创建 WebSocket 服务端（监听 8000 端口，HTTP 路径默认）
srv := websocket.NewServer(":8000", &MyService{},
    kim.WithReadWait(time.Minute*3),
    kim.WithHeartbeat(time.Second*55),
)

// 设置 Acceptor（握手逻辑，验证 Token 等）
srv.SetAcceptor(&MyAcceptor{})

// 设置消息监听器
srv.SetMessageListener(&MyMessageListener{})

// 设置连接断开监听器
srv.SetStateListener(&MyStateListener{})

// 启动服务（内部自动启动 HTTP 服务器，处理 Upgrade）
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
```

### 5.2 创建 WebSocket 客户端（浏览器/Node.js 风格）

```go
import (
    "time"
    "github.com/klintcheng/kim/websocket"
    kim "github.com/klintcheng/kim/internal/kim"
)

// 创建 WebSocket 客户端
cli := websocket.NewClient("client-001", "web-client", websocket.ClientOptions{
    Heartbeat: time.Second * 55,  // 启用心跳
    ReadWait:  time.Minute * 3,
    WriteWait: time.Second * 10,
})

// 设置自定义拨号握手器（实现 kim.Dialer，用于登录认证）
// cli.SetDialer(&MyWSDialer{})

// 连接 WebSocket 服务端（注意使用 ws:// 或 wss:// 前缀）
if err := cli.Connect("ws://127.0.0.1:8000/ws"); err != nil {
    log.Fatal(err)
}
defer cli.Close()

// 发送消息（自动添加 Mask）
err := cli.Send([]byte(`{"cmd":"chat.user.talk","data":"hello"}`))
if err != nil {
    log.Printf("send error: %v", err)
}

// 读取消息循环
for {
    frame, err := cli.Read()
    if err != nil {
        log.Printf("read error: %v", err)
        break
    }
    if frame.GetOpCode() == kim.OpBinary {
        payload := frame.GetPayload()
        // payload 已自动解码 Mask，直接处理业务消息
    }
}
```

### 5.3 带 Meta 的 WebSocket 客户端

```go
meta := map[string]string{
    "version": "1.0.0",
    "platform": "web",
    "token": "jwt-token-here",
}
cli := websocket.NewClientWithProps("user-456", "browser-client", meta, websocket.ClientOptions{
    Heartbeat: time.Second * 55,
})
```

### 5.4 WSS（WebSocket over TLS）示例

```go
// 连接 WSS 地址
err := cli.Connect("wss://your-domain.com/ws")
// 注意：TLS 配置需要在自定义 Dialer 中处理
```

### 5.5 服务端推送消息

```go
// 通过 Channel.Push 推送消息给客户端（服务端消息无 Mask）
func (l *MyMessageListener) Receive(agent kim.Agent, payload []byte) {
    // 处理消息后回复
    response := pkt.Marshal(replyPkt)
    _ = agent.Push(response)
}
```

## 6. 注意事项

### ⚠️ 协议注意事项

1. **Mask 规则**：
   - **客户端→服务端**：必须设置 Mask（本模块 Send 自动处理）
   - **服务端→客户端**：不能设置 Mask（WsConn.WriteFrame 使用 WriteServerMessage 处理）
   - GetPayload() 已自动解密 Masked 数据，无需手动处理

2. **握手要求**：WebSocket 需要 HTTP Upgrade 握手，不能像 TCP 那样直接连接
   - 服务端通过 `ws.Upgrader` 自动处理握手
   - 客户端需要使用合法的 `ws://` 或 `wss://` URL
   - 业务认证（如 Token 验证）在 Acceptor 中完成

3. **Close 帧处理**：Read() 返回 OpClose 时返回 error（"remote side close the channel"），需要处理重连逻辑

4. **与 TCP 帧的差异**：
   - TCP 自定义帧：OpCode(1B) + Length(uint32) + Payload
   - WebSocket：遵循 RFC 6455 标准帧格式，包含 FIN、RSV、Mask 等位

### 性能注意事项

1. **写缓冲区优化**（#15 修复）：`WriteFrame` 写入 bufio.Writer 而非直连 net.Conn，由 writeloop 批量 Flush。**不要改回直连方式**，否则每条消息触发一次系统调用
2. **写缓冲区大小**（#16 修复）：从 1024 扩大到 8192 字节，不要缩小
3. **并发安全**：Client.Send 有互斥锁保护；Read 方法**不支持并发调用**
4. **心跳建议**：浏览器/NAT 环境建议 55 秒心跳，读超时设为 3 分钟（心跳间隔的 3 倍以上）

### 其他注意事项

1. **状态管理**：Client 使用 CAS 原子操作管理连接状态，防止重复 Connect
2. **sync.Once 语义**：Close() 只执行一次，可安全多次调用
3. **URL 验证**：Connect 时先 `url.Parse` 验证地址格式，无效 URL 直接报错
4. **Dialer 必须设置**：Connect 前需要通过 SetDialer 设置实现了 `kim.Dialer` 的握手器，否则为 nil 会 panic
5. **服务名常量**：WebSocket 网关服务名使用 `wire.SNWGateway = "wgateway"`，Consul 注册时使用此名称
6. **TLS 支持**：WSS（WebSocket over TLS）需要自定义 Dialer 配置 `tls.Config`，本模块不直接处理 TLS

### 依赖说明

- 使用 `github.com/gobwas/ws` 作为 WebSocket 协议实现（高性能、零分配）
- 使用 `github.com/gobwas/ws/wsutil` 提供便捷的读写函数
- 不使用 gorilla/websocket，避免其额外开销和依赖问题
