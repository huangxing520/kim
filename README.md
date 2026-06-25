# kim - King IM Cloud

**kim** 是一个使用 Go 语言实现的高性能分布式即时通信（IM）系统。

## 架构简介

kim 采用单二进制多服务架构，编译后生成一个 `kim` 可执行文件，通过不同子命令启动 4 个独立服务：

| 服务 | 子命令 | 协议 | 端口 | 职责 |
|------|--------|------|------|------|
| gateway | `kim gateway` | WebSocket / TCP + gRPC | WS 8000, gRPC 9001, monitor 8001 | 长连接接入层，维护客户端连接 |
| comet | `kim comet` | gRPC | gRPC 8005, monitor 8007 | 消息路由层，登录/聊天/群组/离线消息 |
| logic | `kim logic` | gRPC | gRPC 9002, monitor 8009 | 数据逻辑层，数据库/缓存操作 |
| router | `kim router` | HTTP | HTTP 8100, monitor 8011 | 路由服务，IP 归属地等 |

![architecture](https://p1-juejin.byteimg.com/tos-cn-i-k3u1fbpfcp/2633b07fd1a144d685ceed9be5f64911~tplv-k3u1fbpfcp-watermark.image)

- Web SDK：[Typescript SDK](https://github.com/klintcheng/kim_web_sdk)
- Flutter SDK：[Flutter SDK](https://github.com/szhua/KimSdk)（由 [@szhua](https://github.com/szhua) 提供）

## 技术栈

- Go 1.26
- gRPC + Protocol Buffers
- WebSocket
- Consul（服务发现与注册）
- MySQL / MariaDB（数据持久化）
- Redis（会话与位置缓存）
- Kafka（可选，日志收集）

## 快速开始

### 1. 编译

```bash
make build
```

编译产物输出到 `bin/kim`。

### 2. 启动依赖中间件

使用 Docker Compose 启动 MySQL（MariaDB）、Redis、Consul、Kafka：

```bash
make docker-up
```

启动后可以访问：
- Consul UI: http://localhost:8500
- MySQL: localhost:13306（root / 123456）
- Redis: localhost:16378
- Kafdrop UI: http://localhost:19000

### 3. 启动全部服务

```bash
make run-all
```

这会以后台方式启动全部 4 个服务。验证服务健康状态：

```bash
curl localhost:8001/health
curl localhost:8007/health
curl localhost:8009/health
curl localhost:8011/health
```

### 4. 常用 Make 命令

| 命令 | 说明 |
|------|------|
| `make build` | 编译二进制 |
| `make run-all` | 后台启动全部 4 个服务 |
| `make stop-all` | 停止全部服务 |
| `make status` | 查看服务运行状态 |
| `make run-gateway-fg` | 前台启动 gateway（调试用） |
| `make run-comet-fg` | 前台启动 comet（调试用） |
| `make run-logic-fg` | 前台启动 logic（调试用） |
| `make run-router-fg` | 前台启动 router（调试用） |
| `make logs-gateway` | 查看 gateway 日志 |
| `make logs-comet` | 查看 comet 日志 |
| `make logs-logic` | 查看 logic 日志 |
| `make logs-router` | 查看 router 日志 |
| `make docker-up` | 启动依赖中间件容器 |
| `make docker-down` | 停止依赖中间件容器 |
| `make fmt` | 格式化代码 |
| `make vet` | 静态检查 |
| `make test` | 运行测试 |

## 配置

配置文件位于 `services/*/conf.yaml`，每个服务一份配置。

### 环境变量覆盖

所有配置项都可以通过环境变量覆盖，环境变量格式为 `KIM_<UPPER_SNAKE_CASE>`：

```bash
# 覆盖 Consul 地址
export KIM_CONSUL_URL=http://consul:8500

# 覆盖 Redis 地址
export KIM_REDIS_ADDRS=redis:6379

# 覆盖 Redis 密码
export KIM_REDIS_PASSWORD=your-redis-password

# 覆盖 Kafka brokers（空格分隔的列表）
export KIM_KAFKA_BROKERS="broker1:9092 broker2:9092 broker3:9092"

# 覆盖日志级别
export KIM_LOG_LEVEL=debug
```

环境变量与 YAML 配置字段的映射规则：
- YAML 中的 `.` 和 `_` 统一转换为 `_`
- 全部大写，加 `KIM_` 前缀
- 例：`consul_url` → `KIM_CONSUL_URL`，`kafka.brokers` → `KIM_KAFKA_BROKERS`

## Docker 部署

项目提供统一的多阶段构建 Dockerfile，所有服务使用同一个镜像，通过 command 区分子命令。

### 构建并启动所有服务

先启动依赖中间件：

```bash
docker compose -f docker-compose.yml up -d
```

再启动 kim 服务：

```bash
docker compose -f docker-compose-kim.yml up --build -d
```

每个服务挂载独立的配置文件：
- gateway: `./services/gateway/conf.yaml` → `/etc/kim/conf.yaml`
- comet: `./services/comet/conf.yaml` → `/etc/kim/conf.yaml`
- logic: `./services/logic/conf.yaml` → `/etc/kim/conf.yaml`
- router: `./services/router/conf.yaml` → `/etc/kim/conf.yaml`，同时挂载 `./services/router/data` → `/etc/kim/data`

### 端口说明

| 服务 | 端口映射 |
|------|----------|
| gateway | 8000(WS), 9001(gRPC), 8001(monitor) |
| comet | 8005(gRPC), 8007(monitor) |
| logic | 9002(gRPC), 8009(monitor) |
| router | 8100(HTTP), 8011(monitor) |

## 重新生成 Proto 代码

修改 `.proto` 文件后，需要重新生成 gRPC 代码：

```bash
bash scripts/proto.sh
cd wire && bash build.sh
```

## 目录结构

```
.
├── cmd/kim/          # 主入口（Cobra 子命令）
├── services/         # 4 个服务实现
│   ├── gateway/      # WebSocket/TCP 接入层
│   ├── comet/        # 消息路由层
│   ├── logic/        # 数据逻辑层
│   └── router/       # HTTP 路由服务
├── internal/         # 内部公共包
│   ├── config/       # 配置加载
│   ├── server/       # gRPC/HTTP 服务器封装
│   ├── client/       # gRPC 客户端连接池
│   ├── logger/       # 日志封装
│   ├── naming/       # Consul 服务发现
│   └── trace/        # OpenTelemetry 链路追踪
├── storage/          # Redis 存储实现
├── wire/             # Protocol Buffers 定义与生成代码
├── gen/              # 生成的 gRPC 代码
├── model/            # 公共模型
├── middleware/        # gRPC 中间件
├── scripts/          # 构建/初始化脚本
├── docker-compose.yml      # 依赖中间件（MySQL/Redis/Consul/Kafka）
└── docker-compose-kim.yml  # kim 服务编排
```
