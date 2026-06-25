# L4 部署与文档对齐 — TDD 实施计划

> **日期**：2026-06-26  
> **依赖**：L1+L2+L3 已完成  
> **范围**：5 个子任务  
> **策略**：按子任务顺序执行，每个子任务独立 commit  

---

## 任务总览

| # | 任务 | 文件 | 类型 |
|---|------|------|------|
| L4-1 | 统一 Dockerfile（单镜像多阶段构建） | Dockerfile（新）, Dockerfile_*（删）, docker-compose-kim.yml | 部署 |
| L4-2 | docker-compose 端口对齐 | docker-compose-kim.yml, docker-compose.yml | 部署 |
| L4-3 | README 重写 | README.md | 文档 |
| L4-4 | 环境变量 slice 覆盖修复 | internal/config/config.go | 配置 |
| L4-5 | conf.yaml 统一 + Redis 密码可配 | internal/config/（defaults helper）, services/*/conf.yaml, storage/redis.go | 配置 |

---

## L4-1：统一 Dockerfile（单镜像多阶段构建）

**问题**：4 个旧 Dockerfile 构建路径错误（`./services`、`./cmd/kim` 混合），端口与实际不符（Dockerfile_royal EXPOSE 8080 但 logic 实际监听 9002），Dockerfile_server 构建命令错误。

### 实施步骤

1. **创建新的单一 Dockerfile**：
   - 多阶段构建：`golang:1.26-alpine` AS builder → `scratch`（或 `distroless/static`）
   - CGO_ENABLED=0 静态编译
   - 拷贝 CA 证书（scratch 需要 CA 证书才能做 TLS 验证，不过内部 gRPC 默认无 TLS 暂时可以）
   - 创建 non-root 用户（scratch 无法 adduser，用 `gcr.io/distroless/static:nonroot`）
   - HEALTHCHECK 指向 monitor port 的 /health
   - 不设置 ENTRYPOINT，通过 docker-compose command 指定子命令

2. **删除 4 个旧 Dockerfile**：
   - Dockerfile_gateway
   - Dockerfile_server
   - Dockerfile_royal
   - Dockerfile_router

3. **更新 docker-compose-kim.yml**：
   - 4 个服务全部用同一镜像（build: .）
   - command 区分：`["kim", "gateway", "start"]` 等
   - 挂载配置文件：`./services/gateway/conf.yaml:/etc/kim/conf.yaml:ro`
   - 注意 route.json 和 data 目录也需要挂载

### Commit message
```
chore(docker): unify to single multi-stage Dockerfile, remove 4 old ones

- Multi-stage build: golang:alpine builder -> distroless/static:nonroot
- HEALTHCHECK for each service pointing to monitor port /health
- Single image, different command per service (gateway/comet/logic/router)
- Remove outdated Dockerfile_gateway/server/royal/router
- Update docker-compose-kim.yml with correct ports and volume mounts
```

---

## L4-2：docker-compose 端口对齐

**问题**：docker-compose-kim.yml 中：
- royal(logic) 映射 8080:8080，实际监听 9002
- server(comet) 映射 8005:8005 + 8006:8006，实际 8005(gRPC) + 8007(monitor)
- gateway 映射 8000+8001，缺少 gRPC 9001
- router 8100 正确但缺少 monitor 8011

### 实施步骤

1. **核对各服务端口**（基于 conf.yaml）：
   - gateway: WS 8000, gRPC 9001, monitor 8001
   - comet: gRPC 8005, monitor 8007
   - logic: gRPC 9002, monitor 8009
   - router: HTTP 8100, monitor 8011

2. **更新 docker-compose-kim.yml 端口映射**：
   - gateway: "8000:8000" (WS), "9001:9001" (gRPC), "8001:8001" (monitor/metrics)
   - comet: "8005:8005" (gRPC), "8007:8007" (monitor)
   - logic: "9002:9002" (gRPC), "8009:8009" (monitor)
   - router: "8100:8100" (HTTP), "8011:8011" (monitor)

3. **更新 docker-compose.yml（依赖容器）**：
   - 确认 MySQL 端口映射（当前 docker-up 提示 13306，检查实际映射）
   - Redis 端口映射（提示 16378，检查实际映射）
   - Consul 8500 UI

4. **Makefile help 端口信息同步更新**：
   - run-all 目标中的端口提示更新

### Commit message
```
fix(docker): align docker-compose port mappings with actual service ports

- gateway: 8000(WS) + 9001(gRPC) + 8001(monitor)
- comet:   8005(gRPC) + 8007(monitor)
- logic:   9002(gRPC) + 8009(monitor) (was incorrectly exposing 8080)
- router:  8100(HTTP) + 8011(monitor)
- Update Makefile status output to match
```

---

## L4-3：README 重写

**问题**：README 中启动说明是旧版 `go run main.go gateway/server/royal`，实际是 `bin/kim gateway/comet/logic/router`，且缺少 make 命令说明。

### 实施步骤

1. **重写 README.md**，包含：
   - 项目简介（kim - King IM Cloud，Go 实现的分布式 IM）
   - 架构简述（单二进制，4 个服务：gateway/comet/logic/router）
   - 快速开始：
     1. `make docker-up`（启动 MySQL/Redis/Consul）
     2. `make run-all`（启动全部 4 个服务）
     3. Consul UI: http://localhost:8500
     4. 健康检查: `curl localhost:8001/health`
   - 单服务调试：`make run-gateway-fg` 等
   - 配置：YAML + 环境变量（KIM_ 前缀），如 `KIM_CONSUL_URL=http://host:8500`
   - 重新生成 proto：`bash scripts/proto.sh` + `cd wire && bash build.sh`
   - Docker 部署：`docker compose -f docker-compose-kim.yml up --build`
   - 技术栈：Go 1.26, gRPC, WebSocket, Consul, MySQL, Redis, Kafka

2. **不要**删除 API_REFERENCE.md、TECH_STACK.md、MAKEFILE_GUIDE.md（参考文档保留）

### Commit message
```
docs: rewrite README with correct quickstart and architecture info

- Replace outdated `go run main.go` instructions with Makefile targets
- Add architecture overview (single binary, 4 services)
- Add docker-compose deployment instructions
- Document KIM_ env var prefix for config override
- Keep API_REFERENCE.md/TECH_STACK.md/MAKEFILE_GUIDE.md as-is
```

---

## L4-4：环境变量 slice 覆盖修复

**问题**：Viper 的 AutomaticEnv 对 slice 类型无法直接从环境变量覆盖。例如 `KIM_KAFKA_BROKERS="a:9092 b:9092"` 需要按空格 split 后覆盖配置中的 brokers 列表。

### 代码位置
- `internal/config/config.go` — Load 函数

### TDD 流程

1. **先写测试**：创建 `internal/config/config_slice_test.go`：
   - 测试用例 1：设置 `KIM_KAFKA_BROKERS="h1:9092 h2:9092 h3:9092"` 环境变量，加载配置后 Kafka.Brokers 应为 `["h1:9092", "h2:9092", "h3:9092"]`
   - 测试用例 2：不设置环境变量时使用 YAML 中的值
   - 测试用例 3：空字符串环境变量不覆盖 YAML 值

2. **实现 decodeSliceEnv helper**：
   ```go
   // 在 Load 函数中，Unmarshal 之后调用
   func decodeSliceEnv(v *viper.Viper, key string) {
       envKey := strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
       envKey = "KIM_" + envKey
       if val := os.Getenv(envKey); val != "" {
           parts := strings.Fields(val) // 按空白字符 split
           v.Set(key, parts)
       }
   }
   ```
   - 对 `kafka.brokers` 调用此函数
   - 以后可扩展到其他 slice 字段（如 redis_addrs 但它当前是 string，不改）

3. **验证测试通过**

### Commit message
```
fix(config): support environment variable override for slice fields (kafka.brokers)

Viper's AutomaticEnv cannot override slice types from env vars directly.
Add decodeSliceEnv helper that splits space-separated env values into slices.
Example: KIM_KAFKA_BROKERS="a:9092 b:9092" -> ["a:9092", "b:9092"]
```

---

## L4-5：conf.yaml 统一 + Redis 密码可配

**问题**：
1. router/conf.yaml 在 L2 已补了大部分字段但可能仍有不一致
2. Redis 密码不可配（comet/logic 的 initRedis 只接收 addr）
3. 没有统一的 defaults helper

### 代码位置
- `internal/config/grpc.go` — DefaultGRPCConfig 已有
- `internal/config/resilience.go` — DefaultResilienceConfig 已有
- `services/comet/server.go` — initRedis 调用
- `services/logic/server.go` — initRedis 调用
- `storage/redis.go` — InitRedis 函数签名

### 实施步骤

1. **检查 storage/redis.go**：查看当前 InitRedis 签名，扩展支持 password 参数

2. **修改服务配置结构**：
   - comet/config.go 和 logic/config.go 增加 `redis_password` 字段
   - conf.yaml 中增加 `redis_password: ""`（默认空）

3. **更新 initRedis 调用**：传入 password

4. **验证所有 4 份 conf.yaml 字段一致性**：
   - gateway: 有 service_id/service_name/listen/grpc_listen/public_address/public_port/monitor_port/consul_url/domain/tags/app_secret/log_level/message_g_pool/connection_g_pool/protocol/kafka/resilience/trace/grpc
   - comet: 有 service_id/listen/public_address/public_port/monitor_port/consul_url/redis_addrs/redis_password/log_level/message_g_pool/connection_g_pool/zone/tags/kafka/resilience/trace/grpc/app_secret
   - logic: 有 service_id/node_id/listen/public_address/public_port/monitor_port/consul_url/redis_addrs/redis_password/driver/base_db/message_db/log_level/app_secret/resilience/trace/grpc
   - router: 有 service_id/listen/public_address/public_port/monitor_port/consul_url/log_level/kafka/resilience/trace/grpc（router 无需 redis/db，这是正确的）

5. **router 不需要 redis_password，保持现状**

### Commit message
```
feat(config): add redis_password config option, ensure conf.yaml consistency

- Add redis_password field to comet/logic config structs (default empty)
- Update InitRedis calls to pass password parameter
- Audit and align 4 conf.yaml files for consistent field naming
- router does not use redis, so no redis_password needed
```

---

## 验证清单

L4 全部完成后执行：

```bash
# 构建验证
go build ./...
go vet ./...

# 单元测试（不含集成测试）
go test -race ./internal/... ./wire/...

# Docker 构建验证（可选，需 Docker daemon）
# docker build -t kim:test .

# 配置测试
go test ./internal/config/... -v
```

## 不在 L4 范围

- CI/CD 流水线（L5-6）
- govulncheck/gosec（L5-6）
- 架构重构 package kim→internal/kim（L5-3）
- 目录结构统一（L5-4）
- 依赖升级（L5-5）
