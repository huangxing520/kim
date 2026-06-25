# tcp 模块 - TCP 服务端/客户端实现

## 1. 模块概述

`tcp` 模块实现了基于自定义帧协议的 TCP 服务端和客户端，提供与 `internal/kim` 核心接口兼容的网络层实现。该模块封装了 TCP 连接管理、帧编解码、读写缓冲、心跳保活等功能，可直接用于构建 TCP 网关和 TCP 客户端。

## 2. 协议/架构设计

### 2.1 TCP 帧格式

TCP 是流式协议，本模块使用自定义帧格式进行消息分帧：

```
┌──────────────┬──────────────────────────────────────────┐
│ OpCode(1B)   │             Payload                      │
│              │  [uint32 Length(4B)][Length bytes data]  │
└──────────────┴──────────────────────────────────────────┘
```

- **OpCode**：1 字节，帧类型，定义于 `internal/kim`：
  - `0x0` - Continuation（续帧）
  - `0x1` - Text（文本帧）
  - `0x2` - Binary（二进制帧，主要用于传输 LogicPkt/BasicPkt）
  - `0x8` - Close（关闭帧）
  - `0x9` - Ping（心跳 Ping）
  - `0xa` - Pong（心跳 Pong）
- **Payload**：变长数据，使用 uint32 大端长度前缀

### 2.2 架构分层

```
┌─────────────────────────────────────────────────────────┐
│              业务层 (Acceptor/MessageListener)           │
├─────────────────────────────────────────────────────────┤
│              internal/kim 核心框架 (Server/Channel)      │
├─────────────────────────────────────────────────────────┤
│                   tcp 模块 (本模块)                      │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────┐ │
│  │ tcp.Server  │  │ tcp.Client  │  │ tcp.Upgrader    │ │
│  │ (工厂)      │  │             │  │ (握手升级器)    │ │
│  └─────────────┘  └─────────────┘  └─────────────────┘ │
│  ┌─────────────────────────────────────────────────────┐│
│  │                    tcp.TcpConn                       ││
│  │  (bufio 读写缓冲 + Frame 编解码 + Flush)             ││
│  └─────────────────────────────────────────────────────┘│
├─────────────────────────────────────────────────────────┤
│                   net.TCPConn (标准库)                   │
└─────────────────────────────────────────────────────────┘
```

### 2.3 读写缓冲优化

- 读缓冲区：4096 字节（`bufio.NewReaderSize`）
- 写缓冲区：8192 字节（修复 #16，从 1024 扩大）
- 批量写入：业务层 `WriteFrame` 先写入缓冲区，由 writeloop 统一 `Flush()`，减少系统调用次数

### 2.4 心跳机制

- 客户端配置 `Heartbeat` 间隔后，自动启动 `heartbeatloop` goroutine 周期性发送 Ping
- 读超时设置：设置了心跳时，每次 ReadFrame 前设置读超时（`ReadWait`）
- 写超时设置：每次 Send 时使用 `WriteWait`

## 3. 关键组件

### 3.1 Upgrader - TCP 协议升级器

TCP 无需握手过程，直接将原始 `net.Conn` 包装为带缓冲的 `TcpConn`。

```go
type Upgrader struct{}

func (u *Upgrader) Name() string                                    // 返回 "tcp.Server"
func (u *Upgrader) Upgrade(rawconn net.Conn, rd *bufio.Reader, wr *bufio.Writer) (kim.Conn, error)
```

### 3.2 TcpConn - TCP 连接封装

```go
type TcpConn struct {
    net.Conn               // 嵌入标准 net.Conn
    rd *bufio.Reader       // 读缓冲
    wr *bufio.Writer       // 写缓冲
}
```

主要方法：
- `ReadFrame() (kim.Frame, error)` - 读取一帧：1 字节 OpCode + uint32 长度前缀 Payload
- `WriteFrame(code kim.OpCode, payload []byte) error` - 写入一帧到缓冲区
- `Flush() error` - 刷新写缓冲区
- 全局函数 `WriteFrame(w io.Writer, code kim.OpCode, payload []byte) error` - 通用帧写入

### 3.3 Frame - TCP 帧

```go
type Frame struct {
    OpCode  kim.OpCode
    Payload []byte
}
```

实现 `kim.Frame` 接口：
- `SetOpCode(code kim.OpCode)`
- `GetOpCode() kim.OpCode`
- `SetPayload(payload []byte)`
- `GetPayload() []byte`

### 3.4 Client - TCP 客户端

```go
type Client struct {
    sync.Mutex
    kim.Dialer             // 拨号&握手器（由业务层设置）
    once    sync.Once      // 保证 Close 只执行一次
    id      string
    name    string
    conn    kim.Conn
    state   int32          // 连接状态（CAS 原子操作）
    options ClientOptions
    Meta    map[string]string
}
```

主要方法：
- `Connect(addr string) error` - 拨号连接并握手，启动心跳循环
- `Send(payload []byte) error` - 发送二进制消息（WriteFrame + Flush）
- `Read() (kim.Frame, error)` - 读取一帧，自动检测 Close 帧
- `Close()` - 优雅关闭：发送 Close 帧 → Flush → 关闭连接
- `SetDialer(dialer kim.Dialer)` - 设置握手拨号器
- `ServiceID() / ServiceName() / GetMeta()` - 实现 `kim.Service` 接口

### 3.5 ClientOptions - 客户端配置

```go
type ClientOptions struct {
    Heartbeat time.Duration  // 心跳间隔
    ReadWait  time.Duration  // 读超时
    WriteWait time.Duration  // 写超时
}
```

未设置时使用默认值：
- `ReadWait` → `kim.DefaultReadWait`（3 分钟）
- `WriteWait` → `kim.DefaultWriteWait`（10 秒）

### 3.6 Server 工厂函数

```go
func NewServer(listen string, service kim.ServiceRegistration, options ...kim.ServerOption) kim.Server
```

创建 TCP 协议的 Server 实例，返回 `kim.Server` 接口。

## 4. 核心数据结构

### 4.1 OpCode 定义（来自 internal/kim）

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

### 5.1 创建 TCP 服务端

```go
import (
    "github.com/klintcheng/kim/tcp"
    kim "github.com/klintcheng/kim/internal/kim"
)

// 实现业务服务（ServiceRegistration）
type MyService struct {
    // ...
}

// 创建 TCP 服务端
srv := tcp.NewServer(":8000", &MyService{},
    kim.WithReadWait(time.Minute*3),
    kim.WithHeartbeat(time.Second*55),
)

// 设置 Acceptor（握手逻辑）
srv.SetAcceptor(&MyAcceptor{})

// 设置消息监听器
srv.SetMessageListener(&MyMessageListener{})

// 设置状态监听器
srv.SetStateListener(&MyStateListener{})

// 启动服务
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
```

### 5.2 创建 TCP 客户端

```go
import (
    "time"
    "github.com/klintcheng/kim/tcp"
)

// 创建客户端
cli := tcp.NewClient("client-001", "demo-client", tcp.ClientOptions{
    Heartbeat: time.Second * 55,  // 启用心跳
    ReadWait:  time.Minute * 3,
    WriteWait: time.Second * 10,
})

// 设置自定义拨号握手器（实现 kim.Dialer）
// cli.SetDialer(&MyDialer{})

// 连接服务端
if err := cli.Connect("tcp://127.0.0.1:8000"); err != nil {
    log.Fatal(err)
}
defer cli.Close()

// 发送消息
err := cli.Send([]byte("hello server"))
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
        // 处理 LogicPkt/BasicPkt
    }
}
```

### 5.3 直接使用 TcpConn（底层）

```go
import (
    "net"
    "github.com/klintcheng/kim/tcp"
    kim "github.com/klintcheng/kim/internal/kim"
)

// 接受连接后包装
rawconn, _ := listener.Accept()
conn := tcp.NewConn(rawconn)

// 写入一帧
_ = conn.WriteFrame(kim.OpBinary, []byte("hello"))
_ = conn.Flush()

// 读取一帧
frame, err := conn.ReadFrame()
if err != nil {
    // 处理错误
}
```

### 5.4 带 Meta 的客户端

```go
meta := map[string]string{
    "version": "1.0.0",
    "platform": "android",
}
cli := tcp.NewClientWithProps("user-123", "mobile-client", meta, tcp.ClientOptions{
    Heartbeat: time.Second * 55,
})
```

## 6. 注意事项

### ⚠️ 协议注意事项

1. **帧格式固定**：OpCode 1 字节 + Payload（uint32 长度前缀），不要修改帧格式，否则客户端不兼容
2. **字节序**：所有整数使用大端序（Big Endian）
3. **Close 帧处理**：Read 到 OpClose 时返回 error，调用方需要处理连接关闭逻辑
4. **优雅关闭**：Close() 会先发送 Close 帧再关闭连接，避免直接 RST

### 性能注意事项

1. **写缓冲区**：已从 1024 扩大到 8192 字节（修复 #16），不要改回小值，否则会增加系统调用
2. **Flush 时机**：Send() 内部会自动 Flush；如果批量发送，可考虑在业务层优化 Flush 频率
3. **并发安全**：Client 的 Send 方法有锁保护；Read 方法不支持并发调用（文档注释说明）
4. **心跳设置**：公网环境建议启用心跳（55 秒），防止 NAT 超时断连；读超时应大于心跳间隔（建议 3 倍以上）

### 其他注意事项

1. **TCP 无握手**：Upgrader.Upgrade 直接包装连接，业务握手逻辑需要通过 Acceptor 接口实现
2. **状态管理**：Client 使用 CAS 原子操作管理 state（0=未连接，1=已连接），防止重复 Connect
3. **once 语义**：Close() 使用 sync.Once 保证只关闭一次，可安全多次调用
4. **Dialer 必须设置**：Connect 前需要通过 SetDialer 设置握手拨号器（kim.Dialer 接口），否则 Dialer 为 nil 会 panic
5. **服务名常量**：TCP 网关服务名使用 `wire.SNTGateway = "tgateway"`，Consul 注册时使用此名称
