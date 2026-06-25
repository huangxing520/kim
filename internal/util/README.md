# util 模块

## 模块概述
通用工具函数模块，提供 panic 恢复和安全 goroutine 启动等基础工具，防止 goroutine panic 导致进程崩溃。

## 架构设计
模块封装了 `runtime/debug.Stack()` 的调用模式，在 panic 发生时记录完整调用栈日志，避免因未捕获的 panic 导致整个进程退出。`GoSafe` 函数提供了一种安全启动 goroutine 的便捷方式，自动在 goroutine 入口添加 defer recover。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Recover` | 函数 | 捕获 panic，记录日志（用于 defer） |
| `SafeRecover` | 函数 | 捕获 panic，记录日志，并执行回调 |
| `GoSafe` | 函数 | 安全启动 goroutine，自动添加 panic 恢复 |

## 核心接口

```go
// Recover 在 defer 中调用，捕获 panic 并记录位置和调用栈
func Recover(location string)

// SafeRecover 在 defer 中调用，捕获 panic 后执行回调
func SafeRecover(location string, onRecover func(r interface{}))

// GoSafe 安全启动一个 goroutine，fn 中的 panic 会被自动捕获
func GoSafe(location string, fn func())
```

## 使用示例

### 在 goroutine 中使用 Recover
```go
go func() {
    defer util.Recover("my-worker")
    // 业务逻辑，panic 不会导致进程崩溃
    doWork()
}()
```

### 使用 GoSafe 安全启动 goroutine
```go
util.GoSafe("background-task", func() {
    // 这里的 panic 会被自动捕获并记录日志
    for {
        processItem()
    }
})
```

### 使用 SafeRecover 带回调
```go
go func() {
    var conn net.Conn
    defer util.SafeRecover("connection-handler", func(r interface{}) {
        // panic 后的清理逻辑
        if conn != nil {
            conn.Close()
        }
    })
    handleConnection(conn)
}()
```

### Channel writeloop 中的典型用法
```go
go func() {
    defer util.Recover(fmt.Sprintf("channel.writeloop id=%s", id))
    err := ch.writeloop()
    if err != nil {
        logger.CommonLogger.Info(err)
    }
}()
```

## 依赖关系
- `github.com/klintcheng/kim/internal/logger` - 使用 CommonLogger 记录 panic 日志和调用栈
