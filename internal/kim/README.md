# kim 模块

## 模块概述
整个 IM 系统的核心抽象层与基础实现，定义了 Server、Channel、Acceptor、MessageListener、SessionStorage、Dispatcher、Router、Context 等核心接口，以及 DefaultServer、ChannelImpl、ChannelsImpl、ContextImpl、Router 等默认实现。

## 架构设计
模块采用接口驱动的分层设计：
1. **网络层抽象**：`Conn` 接口统一 TCP/WebSocket 帧读写，`Channel` 代表单个客户端连接，`ChannelImpl` 实现读写分离（独立 writeloop goroutine + ants 协程池处理消息），`ChannelsImpl` 基于 `sync.Map` 管理所有活跃连接。
2. **服务端抽象**：`Server` 接口定义服务生命周期（Start/Shutdown/Push），`DefaultServer` 使用 gobwas/ws 实现 WebSocket/TCP 接入，通过 Upgrader 完成协议升级，Acceptor 处理握手鉴权，Accept 循环接受连接后交由 connHandler 处理。
3. **消息路由层**：`Router` 按 Command 将 LogicPkt 分发到 HandlerFunc 链（类似 HTTP 中间件链），支持全局中间件（Use）和对象池复用 Context（sync.Pool）。
4. **会话存储层**：`SessionStorage` 接口抽象 Session CRUD 和用户位置查询，`Location` 结构体定位用户所在 gateway + channel。
5. **消息上下文**：`Context` 封装单次消息处理的全部信息（Header、Body、Session、Dispatcher、SessionStorage），提供 Resp（回复发送者）和 Dispatch（按 gateway 分组推送）能力。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Service` | 接口 | 基础服务抽象（ServiceID/ServiceName/GetMeta） |
| `ServiceRegistration` | 接口 | 服务注册抽象（组合 Service + 地址/端口/协议） |
| `Server` | 接口 | 通用服务端接口（Start/Shutdown/Push + 监听器设置） |
| `DefaultServer` | 结构体 | Server 接口的 WebSocket/TCP 实现 |
| `Channel` | 接口 | 单个客户端连接抽象（读写循环/推送/关闭） |
| `ChannelImpl` | 结构体 | Channel 接口的实现，读写分离 + 协程池 |
| `ChannelMap` | 接口 | Channel 集合抽象（Add/Remove/Get/All） |
| `ChannelsImpl` | 结构体 | ChannelMap 基于 sync.Map 的并发安全实现 |
| `Acceptor` | 接口 | 连接握手/鉴权（Accept 返回 channelID + Meta） |
| `MessageListener` | 接口 | 上行消息回调（Receive） |
| `StateListener` | 接口 | 连接断开回调（Disconnect） |
| `Agent` | 接口 | 客户端代理（ID/Push/GetMeta） |
| `Conn` | 接口 | WebSocket/协议连接（ReadFrame/WriteFrame/Flush） |
| `SessionStorage` | 接口 | 会话存储抽象（Add/Delete/Get/GetLocations） |
| `Dispatcher` | 接口 | 消息分发抽象（Push 到指定 gateway 的 channels） |
| `Context` | 接口 | 消息处理上下文（Header/ReadBody/Session/Resp/Dispatch/Next） |
| `ContextImpl` | 结构体 | Context 接口的默认实现 |
| `Router` | 结构体 | 消息路由器，按 Command 分发到 HandlerFunc 链 |
| `FuncTree` | 结构体 | 命令到 HandlerFunc 链的映射树 |
| `Location` | 结构体 | 用户位置（ChannelId + GateId），支持二进制序列化 |
| `Event` | 结构体 | 线程安全的一次性事件通知（sync.Once 保证） |
| `Meta` | 类型 | `map[string]string` 连接元数据 |
| `HandlerFunc` | 类型 | 消息处理函数签名 `func(Context)` |

### 常量
| 常量 | 值 | 说明 |
|------|-----|------|
| `DefaultReadWait` | 3 分钟 | 默认读超时 |
| `DefaultWriteWait` | 10 秒 | 默认写超时 |
| `DefaultLoginWait` | 10 秒 | 默认登录握手超时 |
| `DefaultHeartbeat` | 55 秒 | 默认心跳间隔 |
| `DefaultMessageReadPool` | 5000 | 消息处理协程池默认大小 |
| `DefaultConnectionPool` | 5000 | 连接处理协程池默认大小 |

### OpCode
| 常量 | 值 | 说明 |
|------|-----|------|
| `OpBinary` | 0x2 | 二进制帧（业务消息） |
| `OpClose` | 0x8 | 关闭帧 |
| `OpPing` | 0x9 | Ping 帧 |
| `OpPong` | 0xa | Pong 帧（自动回复） |

## 核心接口

```go
// Server 服务端接口
type Server interface {
    ServiceRegistration
    SetAcceptor(Acceptor)
    SetMessageListener(MessageListener)
    SetStateListener(StateListener)
    SetReadWait(time.Duration)
    SetChannelMap(ChannelMap)
    Start() error
    Push(string, []byte) error
    Shutdown(context.Context) error
}

// Channel 客户端连接接口
type Channel interface {
    Conn
    Agent
    Close() error
    Readloop(lst MessageListener) error
    SetWriteWait(time.Duration)
    SetReadWait(time.Duration)
}

// SessionStorage 会话存储接口
type SessionStorage interface {
    Add(session *pkt.Session) error
    Delete(account string, channelId string) error
    Get(channelId string) (*pkt.Session, error)
    GetLocations(account ...string) ([]*Location, error)
    GetLocation(account string, device string) (*Location, error)
    RedisGet(key string) (string, error)
}

// Context 消息处理上下文接口
type Context interface {
    Dispatcher
    SessionStorage
    Header() *pkt.Header
    ReadBody(val proto.Message) error
    Session() Session
    RespWithError(status pkt.Status, err error) error
    Resp(status pkt.Status, body proto.Message) error
    Dispatch(body proto.Message, recvs ...*Location) error
    Next()
    StdContext() context.Context
    WithStdContext(ctx context.Context) Context
}

// Dispatcher 消息分发接口
type Dispatcher interface {
    Push(ctx context.Context, gateway string, channels []string, p *pkt.LogicPkt) error
}
```

## 关键实现细节

### ChannelImpl 读写模型
- **writeloop**：独立 goroutine 从 `writechan`（缓冲 32）取数据，批量 Flush 减少系统调用，收到 closeChan 信号时 drain 剩余数据再退出
- **Readloop**：设置读超时 → ReadFrame → Ping 自动回复 Pong → 业务消息提交到 ants 协程池 → 回调 MessageListener.Receive
- **Push**：非阻塞写入 writechan，channel 已关闭时返回错误
- **状态管理**：atomic int32（0=init, 1=start, 2=closed）

### DefaultServer 连接处理流程
```
net.Listen → Accept 循环
    ↓
connHandler (per connection goroutine)
    ↓
Upgrade (TCP → WebSocket Conn)
    ↓
Accept (Acceptor 握手鉴权，返回 channelID + Meta)
    ↓
检查 channelID 是否重复 → 重复则 OpClose 断开
    ↓
NewChannel（启动 writeloop goroutine）
    ↓
Add 到 ChannelMap → channelTotalGauge.Inc
    ↓
channel.Readloop（阻塞读消息）
    ↓
断开：Remove → StateListener.Disconnect → channel.Close → gauge.Dec
```

### Router 消息路由
- `Use(handlers...)` 注册全局中间件
- `Handle(command, handlers...)` 注册命令处理函数（中间件自动前置）
- `Serve` 从 sync.Pool 获取 Context → reset → 填充 request/dispatcher/storage → serveContext → Next() 执行链 → Put 回池
- 未注册命令返回 `Status_NotImplemented`

### Dispatch 多网关推送
- 接收者按 GateId 分组
- 循环每组调用 Dispatcher.Push 推送到对应 gateway
- 聚合所有错误，不中断后续 gateway 推送（修复群聊跨网关消息丢失问题）

### Location 序列化
- 格式：`[2字节 ChannelId长度][ChannelId][2字节 GateId长度][GateId]`（大端序）
- Bytes() 预分配缓冲区避免热路径上的 bytes.Buffer 分配
- Unmarshal 使用 endian.ReadShortString 解析

## 使用示例

### 创建并启动 DefaultServer
```go
import (
    "github.com/klintcheng/kim/internal/kim"
    "github.com/klintcheng/kim/naming"
)

// 创建服务注册信息
service := naming.NewEntry("gateway-1", "gateway", "ws", "127.0.0.1", 8000)

// 创建服务器
srv := kim.NewServer(":8000", service, wsUpgrader,
    kim.WithMessageGPool(5000),
    kim.WithConnectionGPool(5000),
)

// 设置监听器
srv.SetAcceptor(&myAcceptor{})
srv.SetMessageListener(&myListener{})
srv.SetStateListener(&myStateListener{})

// 启动（阻塞）
if err := srv.Start(); err != nil {
    log.Fatal(err)
}
```

### 使用 Router 注册消息处理器
```go
router := kim.NewRouter()

// 全局中间件
router.Use(middleware.Recover())

// 注册命令处理器
router.Handle("chat.user.talk",
    authHandler,
    talkHandler,
)

// 处理消息
err := router.Serve(ctx, packet, dispatcher, sessionStorage, session)
```

### 在 HandlerFunc 中使用 Context
```go
func talkHandler(ctx kim.Context) {
    var req pkt.TalkReq
    if err := ctx.ReadBody(&req); err != nil {
        ctx.RespWithError(pkt.Status_InvalidPacket, err)
        return
    }

    // 查询接收者位置
    locs, err := ctx.GetLocations(req.GetTo())
    if err != nil {
        ctx.RespWithError(pkt.Status_SystemException, err)
        return
    }

    // 回复发送者
    ctx.Resp(pkt.Status_Success, &pkt.TalkResp{})

    // 分发消息给接收者
    ctx.Dispatch(&pkt.TalkNotify{
        From: ctx.Session().GetAccount(),
        To:   req.GetTo(),
        // ...
    }, locs...)
}
```

### 使用 Event 一次性通知
```go
ready := kim.NewEvent()
go func() {
    // 初始化工作
    ready.Fire() // 通知就绪
}()
<-ready.Done() // 等待就绪
```

### 网络工具函数
```go
// 获取本机 IP
ip := kim.GetLocalIP()

// 从 HTTP 请求获取真实客户端 IP
clientIP := kim.FromRequest(r)
```

## 依赖关系
- `github.com/gobwas/ws` - WebSocket 实现
- `github.com/panjf2000/ants/v2` - goroutine 池
- `github.com/segmentio/ksuid` - 唯一 ID 生成
- `github.com/gobwas/pool/pbufio` - bufio 对象池
- `go.opentelemetry.io/otel` - 链路追踪（Context 支持）
- `google.golang.org/protobuf/proto` - Protobuf 序列化
- `github.com/klintcheng/kim/internal/logger` - 日志
- `github.com/klintcheng/kim/internal/util` - Recover 工具
- `github.com/klintcheng/kim/wire/pkt` - 消息包定义
- `github.com/klintcheng/kim/wire` - Meta 常量
- `github.com/prometheus/client_golang/prometheus` - channel_total gauge

### 被依赖关系
- `internal/naming` - ServiceRegistration 接口
- `internal/client` - 服务发现 watch 使用 ServiceRegistration
- `storage` - SessionStorage 接口实现
- `services/*` - 所有服务使用本模块的接口和实现
