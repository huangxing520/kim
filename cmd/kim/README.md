# kim - 主程序入口（Cobra CLI）

## 模块概述

`cmd/kim` 是 King IM Cloud 系统的统一主程序入口，采用单二进制多服务架构。通过 Cobra CLI 框架实现一个二进制文件包含所有服务，使用子命令方式启动不同服务。

版本：`v2.0.0`

## 架构设计

kim 使用单二进制架构，所有服务编译进同一个可执行文件，通过子命令区分启动哪个服务：

```
┌─────────────────────────────────────────────────────────────┐
│                         kim 二进制                            │
│                    (cmd/kim/main.go)                         │
└──────────────────────────┬──────────────────────────────────┘
                           │
            ┌──────────────┼──────────────┐
            │              │              │
┌───────────▼───┐  ┌───────▼────┐  ┌──────▼─────┐  ┌──────────▼───┐
│   gateway     │  │   comet    │  │   logic    │  │   router     │
│  子命令        │  │  子命令     │  │  子命令     │  │  子命令       │
└───────┬───────┘  └──────┬─────┘  └──────┬─────┘  └──────┬───────┘
        │                 │               │               │
        └─────────────────┴───────┬───────┴───────────────┘
                                  │
                        ┌─────────▼─────────┐
                        │  signal.NotifyContext
                        │  (SIGINT/SIGTERM)
                        │  优雅关闭          │
                        └───────────────────┘
```

设计优势：
- **统一部署**：只需分发一个二进制文件
- **版本一致**：所有服务版本同步，不存在兼容性问题
- **便捷开发**：本地调试只需一个命令切换不同服务
- **共享基础设施**：internal 模块代码在各服务间共享

## 关键组件

### main.go - 主入口

`cmd/kim/main.go`

```go
const version = "v2.0.0"

func main() {
    flag.Parse()

    root := &cobra.Command{
        Use:     "kim",
        Version: version,
        Short:   "King IM Cloud",
    }

    // 使用 signal.NotifyContext 支持优雅关闭
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    root.AddCommand(gatewaycmd.NewStartCmd(ctx, version))
    root.AddCommand(cmd.NewStartCmd(ctx, version))
    root.AddCommand(logiccmd.NewStartCmd(ctx, version))
    root.AddCommand(routercmd.NewStartCmd(ctx, version))

    if err := root.Execute(); err != nil {
        fmt.Fprintf(os.Stderr, "Could not run command: %v\n", err)
        os.Exit(1)
    }
}
```

核心逻辑：
1. 解析 flag
2. 创建 root Cobra 命令
3. 使用 `signal.NotifyContext` 监听 SIGINT/SIGTERM 信号
4. 注册四个服务子命令（gateway/comet/logic/router）
5. 执行命令，出错时输出到 stderr 并退出码 1
6. 收到信号时 ctx 被取消，各服务的 goroutine 会触发优雅关闭流程（10 秒超时）

### 优雅关闭机制

每个服务的 start.go 中都有统一的关闭逻辑：

```go
go func() {
    <-ctx.Done()
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = srv.Stop(shutdownCtx)
}()
```

流程：
1. 主进程收到 SIGINT（Ctrl+C）或 SIGTERM（kill/docker stop）
2. `signal.NotifyContext` 取消 ctx
3. 各服务的 goroutine 从 `<-ctx.Done()` 唤醒
4. 创建 10 秒超时的 shutdownCtx
5. 调用对应服务的 `Stop()` 方法执行优雅关闭：
   - 从 Consul 反注册服务
   - 关闭 gRPC 连接池
   - GracefulStop gRPC Server
   - 关闭 WS/TCP 连接
   - 关闭日志、数据库等资源

## Cobra 命令结构

### Root 命令

```
kim - King IM Cloud v2.0.0
```

### 子命令

| 子命令 | 服务 | 说明 |
|--------|------|------|
| `gateway` | WS/TCP 接入网关 | 启动 Gateway 服务 |
| `comet` | 聊天/登录/群组业务 | 启动 Comet 服务 |
| `logic` | 消息持久化逻辑 | 启动 Logic 服务 |
| `router` | IP 区域路由 | 启动 Router 服务 |

### 全局选项

| 选项 | 说明 |
|------|------|
| `-h`, `--help` | 显示帮助信息 |
| `-v`, `--version` | 显示版本信息 |

### 各子命令选项

#### gateway 子命令

```
kim gateway [flags]
```

| 选项 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/gateway/conf.yaml` | 配置文件路径 |
| `--route` | `-r` | `services/gateway/route.json` | 路由配置文件路径 |
| `--protocol` | `-p` | `ws` | 接入协议：ws 或 tcp |

#### comet 子命令

```
kim comet [flags]
```

| 选项 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/comet/conf.yaml` | 配置文件路径 |

#### logic 子命令

```
kim logic [flags]
```

| 选项 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/logic/conf.yaml` | 配置文件路径 |

#### router 子命令

```
kim router [flags]
```

| 选项 | 短参 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `services/router/conf.yaml` | 配置文件路径 |
| `--data` | `-d` | `services/router/data` | 数据文件目录 |

## 启动方式

### 编译

```bash
# 编译为 bin/kim
make build

# 或直接 go build
go build -o bin/kim ./cmd/kim
```

### 服务启动顺序

全量部署时建议按以下顺序启动：

1. **基础设施**（需提前启动）：
   - MySQL（3306）
   - Redis（6379）
   - Consul（8500）
   - Kafka（可选，29092）

2. **业务服务**（顺序启动）：
   ```bash
   # 1. 先启动 Logic（数据层）
   ./bin/kim logic -c services/logic/conf.yaml &

   # 2. 再启动 Comet（业务层，依赖 Logic）
   ./bin/kim comet -c services/comet/conf.yaml &

   # 3. 启动 Gateway（接入层，依赖 Comet）
   ./bin/kim gateway -c services/gateway/conf.yaml &

   # 4. 启动 Router（边缘层，依赖 Gateway 和 Consul）
   ./bin/kim router -c services/router/conf.yaml &
   ```

### Make 命令快捷启动

```bash
# 启动基础设施（Docker）
make docker-up

# 后台启动所有 4 个服务
make run-all

# 查看服务状态
make status

# 查看某服务日志
make logs-gateway
make logs-comet
make logs-logic
make logs-router

# 前台单服务调试
make run-gateway-fg
make run-comet-fg
make run-logic-fg
make run-router-fg

# 停止所有服务
make stop-all

# 停止基础设施
make docker-down
```

### 直接命令行启动示例

```bash
# 启动 Gateway（默认配置）
./bin/kim gateway

# 启动 Gateway（TCP 协议，自定义配置）
./bin/kim gateway -c conf/gateway-prod.yaml -p tcp

# 启动 Logic（生产配置）
./bin/kim logic -c /etc/kim/logic.yaml

# 启动 Router（自定义数据目录）
./bin/kim router -d /opt/kim/data
```

### 验证服务启动

```bash
# 健康检查
curl http://127.0.0.1:8001/health  # Gateway monitor
curl http://127.0.0.1:8007/health  # Comet monitor
curl http://127.0.0.1:8009/health  # Logic monitor
curl http://127.0.0.1:8100/health  # Router health

# Router API 测试
curl http://127.0.0.1:8100/api/lookup/test-token

# Consul 服务列表（确认所有服务已注册）
curl http://127.0.0.1:8500/v1/agent/services
```

## 依赖关系

### 服务子命令包依赖

| 包 | 用途 |
|----|------|
| `services/gateway/cmd` | Gateway 子命令实现 |
| `services/comet/cmd` | Comet 子命令实现（别名为 `cmd`） |
| `services/logic/cmd` | Logic 子命令实现 |
| `services/router/cmd` | Router 子命令实现 |

### 第三方库依赖

| 库 | 用途 |
|----|------|
| `github.com/spf13/cobra` | CLI 命令行框架 |
| 标准库 `context` | 上下文传递 |
| 标准库 `flag` | 命令行 flag 解析 |
| 标准库 `os/signal` | 信号处理 |
| 标准库 `syscall` | 系统调用（SIGINT/SIGTERM） |

### 不直接依赖

main.go 不直接依赖 internal 模块或具体业务实现，所有初始化逻辑都封装在各服务的 `NewStartCmd` 中，保持入口层简洁。

## 配置约定

各服务统一使用以下配置约定：

### 配置加载

- 通过 `internal/config.Load` 使用 Viper 加载 YAML 配置
- 支持 `KIM_` 前缀的环境变量覆盖配置项
- 配置键名使用 snake_case
- 各服务配置文件默认在 `services/<svc>/conf.yaml`

### 日志约定

- 日志文件默认输出到 `./data/<svc>.log`
- 各服务使用专属 logger：`logger.GatewayLogger`、`logger.CometLogger`、`logger.LogicLogger`、`logger.RouterLogger`
- 通用日志使用 `logger.CommonLogger`

### 服务名约定

Consul 注册服务名常量定义在 `wire/definitions.go`，**不可修改**：

| 常量 | 值 | 服务 |
|------|----|------|
| `wire.SNWGateway` | `wgateway` | Gateway gRPC 服务名 |
| `wire.SNChat` | `chat` | Comet gRPC 服务名 |
| `wire.SNService` | `royal` | Logic gRPC 服务名 |

### 端口约定

| 服务 | 业务端口 | gRPC 端口 | Monitor 端口 |
|------|----------|-----------|--------------|
| Gateway | :8000 (WS/TCP) | :9001 | :8001 |
| Comet | - | :8005 | :8007 |
| Logic | - | :9002 | :8009 |
| Router | :8100 (HTTP) | - | :8100 (同端口) |
