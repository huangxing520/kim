# Kim IM 项目 Makefile 使用指南

本项目的 [Makefile](./Makefile) 封装了 4 个微服务（gateway / server / service / router）的构建、启动、停止、日志查看等常用操作，便于本地开发与调试。

---

## 一、环境要求

| 工具 | 用途 | 说明 |
|------|------|------|
| Go ≥ 1.16 | 编译主程序 | 需配置 `GOPATH` 与 `GOPROXY` |
| GNU Make | 执行 Makefile | Windows 推荐 Git Bash / MSYS2 / WSL |
| Docker + Docker Compose | 启动 MySQL/Redis/Consul 依赖 | 可选，也可使用本机原生服务 |

> **Windows 用户注意**：Makefile 使用了 shell 后台运行（`&`）和 `kill` 命令，必须在 Git Bash / MSYS2 / WSL 环境下运行 `make`，不能在 CMD 或 PowerShell 中直接运行。

---

## 二、项目服务概览

项目入口在 [services/main.go](./services/main.go)，通过 cobra 子命令区分 4 个服务：

| 服务 | 子命令 | 监听端口 | 配置文件 | 功能 |
|------|--------|----------|----------|------|
| gateway | `gateway` | :8000 | `services/gateway/conf.yaml` | WebSocket/TCP 接入网关 |
| server | `server` | :8005 | `services/server/conf.yaml` | 聊天/登录/群组业务逻辑 |
| service | `royal` | :8080 | `services/service/conf.yaml` | HTTP API + MySQL 数据服务 |
| router | `router` | :8100 | `services/router/conf.yaml` | IP 区域路由服务 |

### 服务依赖关系

```
                ┌─────────┐
                │  MySQL  │ ← service (royal)
                └────┬────┘
                     │
┌──────────┐    ┌────▼─────┐    ┌──────────┐
│  Redis   │◄──►│  service │◄──►│  server  │
└────┬─────┘    └──────────┘    └────┬─────┘
     │                               │
┌────▼─────┐                   ┌────▼─────┐
│  Consul  │◄─────────────────►│  gateway │ → 客户端
└────┬─────┘                   └──────────┘
     │
┌────▼─────┐
│  router  │ → 客户端首次接入路由
└──────────┘
```

- **service** 依赖 MySQL、Redis、Consul
- **server** 依赖 service（HTTP）、Redis、Consul
- **gateway** 依赖 server、Consul
- **router** 依赖 Consul

---

## 三、快速开始

### 1. 完整启动流程

```bash
# 1. 启动依赖容器（MySQL/Redis/Consul）
make docker-up

# 2. 后台启动全部 4 个 kim 服务
make run-all

# 3. 查看运行状态
make status

# 4. 查看某个服务的实时日志
make logs-gateway

# 5. 停止全部服务
make stop-all

# 6. 停止依赖容器
make docker-down
```

### 2. 首次运行前的准备

如果是首次拉取代码，建议先下载依赖：

```bash
make deps
```

---

## 四、命令详解

### 4.1 构建相关

| 命令 | 说明 |
|------|------|
| `make build` | 编译主二进制文件到 `bin/kim` |
| `make build-all` | 编译并创建日志/PID 目录 |
| `make deps` | 下载 Go 依赖 |
| `make tidy` | 整理 `go.mod` / `go.sum` |

**示例**：

```bash
make build
# 输出：==> 编译完成: bin/kim
```

编译产物为 `bin/kim`，可通过 `./bin/kim --help` 查看子命令。

---

### 4.2 启动单个服务（后台）

每个 `run-*` 命令都会：
1. 自动编译二进制
2. 在 `services/` 目录下后台运行（工作目录与配置文件相对路径匹配）
3. 将日志输出到 `logs/<服务名>.log`
4. 将进程 PID 写入 `.pid/<服务名>.pid`

| 命令 | 服务 | 端口 |
|------|------|------|
| `make run-gateway` | gateway 网关 | :8000 |
| `make run-server` | server 业务 | :8005 |
| `make run-service` | service 数据 | :8080 |
| `make run-router` | router 路由 | :8100 |

**示例**：

```bash
make run-gateway
# ==> 启动 gateway 服务...
# ==> gateway 已启动, PID: 12345
# ==> 日志: logs/gateway.log
```

---

### 4.3 启动单个服务（前台调试）

前台运行会直接占用终端，日志输出到屏幕，**Ctrl+C 退出**。适合调试断点。

| 命令 | 说明 |
|------|------|
| `make run-gateway-fg` | 前台启动 gateway |
| `make run-server-fg` | 前台启动 server |
| `make run-service-fg` | 前台启动 service |
| `make run-router-fg` | 前台启动 router |

**示例**：

```bash
make run-server-fg
# 日志直接输出到终端，便于观察启动过程和调试
```

---

### 4.4 批量启动

```bash
make run-all
```

按依赖顺序后台启动 4 个服务：

```
service (royal)  → :8080   # 数据层先启动
router           → :8100   # 路由服务
server  (chat)   → :8005   # 业务层
gateway (ws)     → :8000   # 网关最后启动
```

> **注意**：`run-all` 不会等待前一个服务完全就绪，如果 Consul/MySQL 未启动，server/gateway 可能注册失败。建议先执行 `make docker-up`。

---

### 4.5 停止服务

#### 停止单个服务

| 命令 | 说明 |
|------|------|
| `make stop-gateway` | 停止 gateway |
| `make stop-server` | 停止 server |
| `make stop-service` | 停止 service |
| `make stop-router` | 停止 router |

停止逻辑：
1. 读取 `.pid/<服务名>.pid` 中的 PID
2. 检查进程是否存活，存活则发送 `kill` 信号
3. 删除 PID 文件

#### 停止全部服务

```bash
make stop-all
```

按 gateway → server → service → router 的顺序停止。

---

### 4.6 状态查看

```bash
make status
```

输出示例：

```
==> 服务运行状态:
    gateway    RUNNING  PID: 12345
    server     RUNNING  PID: 12346
    service    DEAD     PID: 12347 (进程已退出)
    router     STOPPED  (无 PID 文件)
```

状态说明：
- **RUNNING**：进程正常运行
- **DEAD**：PID 文件存在但进程已退出（异常崩溃）
- **STOPPED**：未启动过或已正常停止

---

### 4.7 日志查看

实时跟踪日志（`tail -f`，Ctrl+C 退出）：

| 命令 | 日志文件 |
|------|----------|
| `make logs-gateway` | `logs/gateway.log` |
| `make logs-server` | `logs/server.log` |
| `make logs-service` | `logs/service.log` |
| `make logs-router` | `logs/router.log` |

**示例**：

```bash
make logs-gateway
# 实时输出 gateway 日志
```

---

### 4.8 代码质量

| 命令 | 说明 |
|------|------|
| `make fmt` | 格式化全部 Go 代码 |
| `make vet` | 静态检查 |
| `make test` | 运行全部单元测试 |

**示例**：

```bash
make vet
# ==> 静态检查...
# go vet ./...
```

---

### 4.9 Docker 依赖管理

项目根目录的 [docker-compose.yml](./docker-compose.yml) 定义了 3 个依赖容器：

| 容器 | 端口 | 凭据 |
|------|------|------|
| MySQL 8.0 | 3306 | root / 123456 |
| Redis 6.2 | 6379 | 无密码 |
| Consul | 8500 (UI) / 8300 / 53 | - |

| 命令 | 说明 |
|------|------|
| `make docker-up` | 启动 MySQL/Redis/Consul 容器 |
| `make docker-down` | 停止并移除容器 |

**示例**：

```bash
make docker-up
# ==> 启动依赖容器 (MySQL/Redis/Consul)...
# ==> 依赖容器已启动
#     MySQL:  localhost:3306  (root/123456)
#     Redis:  localhost:6379
#     Consul: localhost:8500  (UI: http://localhost:8500)
```

启动后可访问 Consul UI：[http://localhost:8500](http://localhost:8500)

---

### 4.10 清理

| 命令 | 说明 |
|------|------|
| `make clean` | 停止全部服务并清理 `bin/` 和 `.pid/` |
| `make clean-all` | 额外清理 `logs/` 目录 |

**示例**：

```bash
make clean
# ==> 停止全部服务...
# ==> 清理构建产物...
# ==> 清理完成
```

---

## 五、目录结构说明

执行 `make run-all` 后，项目会生成以下目录：

```
kim/
├── Makefile              # 本文件
├── bin/                  # 编译产物（make build 生成）
│   └── kim               # 主二进制
├── logs/                 # 运行日志（make run-* 生成）
│   ├── gateway.log
│   ├── server.log
│   ├── service.log
│   └── router.log
├── .pid/                 # PID 文件（make run-* 生成）
│   ├── gateway.pid
│   ├── server.pid
│   ├── service.pid
│   └── router.pid
└── services/             # 服务代码与配置（工作目录）
    ├── main.go
    ├── gateway/conf.yaml
    ├── server/conf.yaml
    ├── service/conf.yaml
    └── router/
        ├── conf.yaml
        └── data/
```

> **重要**：服务实际在 `services/` 目录下运行，因此配置文件中的相对路径（如 `./gateway/conf.yaml`）能正确解析。

---

## 六、配置文件说明

各服务的配置文件位于 `services/<服务名>/conf.yaml`，启动前需根据本机环境修改：

### 6.1 service（royal）配置

编辑 [services/service/conf.yaml](./services/service/conf.yaml)：

```yaml
ConsulURL: localhost:8500
RedisAddrs: localhost:6379
# 修改为你的 MySQL 连接信息
BaseDb: root:123456@tcp(localhost:3306)/kim_base?charset=utf8mb4&parseTime=True&loc=Local
MessageDb: root:123456@tcp(localhost:3306)/kim_message?charset=utf8mb4&parseTime=True&loc=Local
```

> **注意**：`make docker-up` 启动的 MySQL 密码为 `123456`，需与配置文件一致。首次运行需手动创建数据库 `kim_base` 和 `kim_message`，service 启动时会自动建表。

### 6.2 gateway 配置

编辑 [services/gateway/conf.yaml](./services/gateway/conf.yaml)：

```yaml
ConsulURL: localhost:8500    # 修改为你的 Consul 地址
```

### 6.3 server 配置

编辑 [services/server/conf.yaml](./services/server/conf.yaml)：

```yaml
ConsulURL: localhost:8500
RedisAddrs: localhost:6379
RoyalURL: http://localhost:8080   # service 服务的地址
```

---

## 七、常见场景

### 场景 1：本地开发调试

```bash
# 1. 启动依赖
make docker-up

# 2. 前台启动 service（便于看日志和断点）
make run-service-fg

# 3. 另开终端，后台启动其他服务
make run-router
make run-server
make run-gateway

# 4. 修改代码后重新编译
make build

# 5. 停止并重启某个服务
make stop-gateway
make run-gateway
```

### 场景 2：完整集成测试

```bash
# 一键启动全部
make docker-up
make run-all

# 查看状态
make status

# 查看日志
make logs-gateway

# 测试完成后一键停止
make stop-all
make docker-down
```

### 场景 3：代码提交前检查

```bash
make fmt      # 格式化
make vet      # 静态检查
make test     # 单元测试
```

### 场景 4：服务异常排查

```bash
# 1. 查看状态，发现 service 是 DEAD
make status

# 2. 查看日志定位原因
make logs-service

# 3. 通常是 MySQL/Redis 未启动，检查依赖
docker ps

# 4. 重启依赖和服务
make docker-up
make stop-service
make run-service
```

---

## 八、命令速查表

| 类别 | 命令 | 说明 |
|------|------|------|
| **构建** | `make build` | 编译二进制 |
| | `make deps` | 下载依赖 |
| **启动（后台）** | `make run-gateway` | 启动网关 :8000 |
| | `make run-server` | 启动业务 :8005 |
| | `make run-service` | 启动数据 :8080 |
| | `make run-router` | 启动路由 :8100 |
| | `make run-all` | 启动全部 |
| **启动（前台）** | `make run-*-fg` | 前台调试启动 |
| **停止** | `make stop-*` | 停止单个 |
| | `make stop-all` | 停止全部 |
| **监控** | `make status` | 查看状态 |
| | `make logs-*` | 查看日志 |
| **代码质量** | `make fmt` | 格式化 |
| | `make vet` | 静态检查 |
| | `make test` | 单元测试 |
| **Docker** | `make docker-up` | 启动依赖容器 |
| | `make docker-down` | 停止依赖容器 |
| **清理** | `make clean` | 清理构建产物 |
| | `make clean-all` | 清理全部生成文件 |
| **帮助** | `make help` | 显示帮助 |

---

## 九、常见问题

### Q1：Windows 下 `make` 命令不存在？

**A**：Windows 不自带 `make`，需通过以下任一方式安装：
- **Git Bash**：安装 [Git for Windows](https://git-scm.com/download/win) 后自带
- **MSYS2**：`pacman -S make`
- **WSL**：在 WSL 中 `sudo apt install make`
- **Chocolatey**：`choco install make`

### Q2：启动服务后立即退出，状态显示 DEAD？

**A**：通常是依赖未就绪。排查步骤：
1. `make docker-up` 确保容器启动
2. `docker ps` 确认 MySQL/Redis/Consul 运行中
3. `make logs-<服务名>` 查看具体错误
4. 检查 `services/*/conf.yaml` 中的连接地址和凭据

### Q3：Consul 注册失败？

**A**：检查 Consul 是否启动（[http://localhost:8500](http://localhost:8500)），并确认配置文件中 `ConsulURL` 地址正确。

### Q4：MySQL 连接被拒绝？

**A**：
1. 确认 MySQL 容器已启动：`docker ps | grep mysql`
2. 确认数据库已创建：`mysql -h127.0.0.1 -uroot -p123456 -e "SHOW DATABASES"`
3. 首次需手动创建：`CREATE DATABASE kim_base; CREATE DATABASE kim_message;`
4. 检查 `services/service/conf.yaml` 中 `BaseDb` / `MessageDb` 的密码是否为 `123456`

### Q5：如何修改服务监听端口？

**A**：编辑对应服务的 `conf.yaml` 中的 `Listen` 字段。例如 gateway 改为 9000：

```yaml
# services/gateway/conf.yaml
Listen: ":9000"
PublicPort: 9000
```

### Q6：`make run-all` 后 gateway 注册不到 server？

**A**：`run-all` 是并行启动，server 可能还未注册到 Consul。解决：
1. 先 `make run-service && make run-server`，等待 5 秒
2. 再 `make run-gateway`
3. 或访问 [Consul UI](http://localhost:8500) 查看服务列表

### Q7：如何查看某个服务的完整启动参数？

**A**：执行 `./bin/kim <子命令> --help`，例如：

```bash
./bin/kim gateway --help
# 查看 gateway 的所有启动参数
```

---

## 十、完整命令列表

执行 `make help` 可查看所有可用命令的简要说明：

```bash
make help
```

输出包含构建、启动、停止、监控、代码质量、Docker、清理等全部命令的分类说明。
