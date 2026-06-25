# middleware 模块

## 模块概述
业务层中间件模块，当前提供 panic 恢复中间件，用于 Router HandlerFunc 链中捕获业务 handler 的 panic，防止单个消息处理 panic 导致进程崩溃。

## 架构设计
中间件遵循 `kim.HandlerFunc` 签名，通过 `ctx.Next()` 调用链中的下一个处理器，在 defer 中使用 `recover()` 捕获 panic。panic 发生时记录调用栈（使用 `runtime.Caller` 逐级收集文件和行号）、ChannelId、Command、Seq 等上下文信息到日志，并向客户端返回 `SystemException` 错误响应。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Recover` | 函数 | 返回 panic 恢复中间件 HandlerFunc |

## 核心接口

```go
// Recover 返回 panic 恢复中间件
func Recover() kim.HandlerFunc
```

## 配置说明
本模块无额外配置，直接通过 `Router.Use()` 注册为全局中间件即可。panic 恢复时：
- 收集调用栈（从 Caller(1) 开始逐级收集直到返回 false）
- 记录字段：ChannelId、Command、Sequence、Caller 调用栈
- 响应：`pkt.Status_SystemException` + `ErrorResp{Message: "SystemException"}`
- 使用 `logger.CometLogger` 记录错误日志

## 使用示例

### 注册为全局中间件
```go
import (
    "github.com/klintcheng/kim/internal/kim"
    "github.com/klintcheng/kim/middleware"
)

router := kim.NewRouter()

// 注册 Recover 中间件（建议作为第一个全局中间件，保证所有 handler 的 panic 都被捕获）
router.Use(middleware.Recover())

// 注册业务 handler
router.Handle("chat.user.talk", talkHandler)
```

### 单独为某个命令注册
```go
router.Handle("some.risky.command",
    middleware.Recover(),  // 仅对该命令生效
    riskyHandler,
)
```

## 依赖关系
- `github.com/klintcheng/kim/internal/kim` - HandlerFunc、Context 接口
- `github.com/klintcheng/kim/internal/logger` - CometLogger 记录 panic 日志
- `github.com/klintcheng/kim/wire/pkt` - Status、ErrorResp 响应结构
