# Kim IM 项目 Makefile
# 用于构建、运行、停止各个服务
# 注意：Windows 环境下请使用 GNU Make（如通过 MSYS2/Git Bash/WSL 运行）

# ==================== 变量定义 ====================

# Go 相关变量
GO              ?= go
GOFLAGS         ?= -mod=readonly
BINARY_NAME     := kim
MAIN_PACKAGE    := ./services
BUILD_DIR       := bin
CONFIG_DIR      := services

# 各服务配置文件路径
GATEWAY_CONF    := $(CONFIG_DIR)/gateway/conf.yaml
SERVER_CONF     := $(CONFIG_DIR)/server/conf.yaml
SERVICE_CONF    := $(CONFIG_DIR)/service/conf.yaml
ROUTER_CONF     := $(CONFIG_DIR)/router/conf.yaml
ROUTER_DATA     := $(CONFIG_DIR)/router/data
GATEWAY_ROUTE   := $(CONFIG_DIR)/gateway/route.json

# 日志目录
LOG_DIR         := logs

# PID 文件目录（用于 stop 命令记录进程 PID）
PID_DIR         := .pid

# ==================== 伪目标 ====================

.PHONY: all build run-gateway run-server run-service run-router \
        stop-gateway stop-server stop-service stop-router stop-all \
        run-all build-all clean deps fmt vet test \
        docker-up docker-down help

# ==================== 默认目标 ====================

all: build

# ==================== 构建相关 ====================

## build: 编译主二进制文件到 bin/ 目录
build:
	@echo "==> 正在编译 $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)
	@echo "==> 编译完成: $(BUILD_DIR)/$(BINARY_NAME)"

## build-all: 编译并准备所有运行所需文件
build-all: build
	@mkdir -p $(LOG_DIR) $(PID_DIR)
	@echo "==> 准备工作目录完成"

# ==================== 依赖管理 ====================

## deps: 下载/更新依赖
deps:
	@echo "==> 下载依赖..."
	$(GO) mod download
	@echo "==> 依赖下载完成"

## tidy: 整理 go.mod
tidy:
	$(GO) mod tidy

# ==================== 单服务运行 ====================

## run-gateway: 启动 gateway 网关服务（websocket/tcp 接入）
run-gateway: build-all
	@echo "==> 启动 gateway 服务..."
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) gateway \
		-c ./gateway/conf.yaml \
		-r ./gateway/route.json \
		-p ws \
		> ../$(LOG_DIR)/gateway.log 2>&1 & \
		echo pwd & \
		echo $$! > ../$(PID_DIR)/gateway.pid
	@echo "==> gateway 已启动, PID: $$(cat $(PID_DIR)/gateway.pid)"
	@echo "==> 日志: $(LOG_DIR)/gateway.log"

## run-server: 启动 server 消息服务（聊天/登录/群组业务）
run-server: build-all
	@echo "==> 启动 server 服务..."
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) server \
		-c ./server/conf.yaml \
		-s chat \
		> ../$(LOG_DIR)/server.log 2>&1 & \
		echo $$! > ../$(PID_DIR)/server.pid
	@echo "==> server 已启动, PID: $$(cat $(PID_DIR)/server.pid)"
	@echo "==> 日志: $(LOG_DIR)/server.log"

## run-service: 启动 service 数据服务（HTTP API + MySQL）
run-service: build-all
	@echo "==> 启动 service 服务..."
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) royal \
		-c ./service/conf.yaml \
		> ../$(LOG_DIR)/service.log 2>&1 & \
		echo $(PWD) & \
		echo $$! > ../$(PID_DIR)/service.pid
	@echo "==> service 已启动, PID: $$(cat $(PID_DIR)/service.pid)"
	@echo "==> 日志: $(LOG_DIR)/service.log"

## run-router: 启动 router 路由服务（IP 区域路由）
run-router: build-all
	@echo "==> 启动 router 服务..."
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) router \
		-c ./router/conf.yaml \
		-d ./router/data \
		> ../$(LOG_DIR)/router.log 2>&1 & \
		echo $$! > ../$(PID_DIR)/router.pid
	@echo "==> router 已启动, PID: $$(cat $(PID_DIR)/router.pid)"
	@echo "==> 日志: $(LOG_DIR)/router.log"

# ==================== 前台运行（调试用） ====================

## run-gateway-fg: 前台启动 gateway（用于调试）
run-gateway-fg: build
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) gateway \
		-c ./gateway/conf.yaml \
		-r ./gateway/route.json \
		-p ws

## run-server-fg: 前台启动 server（用于调试）
run-server-fg: build
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) server \
		-c ./server/conf.yaml \
		-s chat

## run-service-fg: 前台启动 service（用于调试）
run-service-fg: build
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) royal \
		-c ./service/conf.yaml

## run-router-fg: 前台启动 router（用于调试）
run-router-fg: build
	@cd services && ../$(BUILD_DIR)/$(BINARY_NAME) router \
		-c ./router/conf.yaml \
		-d ./router/data

# ==================== 启动全部服务 ====================

## run-all: 后台启动全部 4 个服务（建议先 docker-up 启动依赖）
run-all: build-all run-service run-router run-server run-gateway
	@echo ""
	@echo "==> 全部服务已启动:"
	@echo "    service (royal)  -> :8080  PID: $$(cat $(PID_DIR)/service.pid)"
	@echo "    router           -> :8100  PID: $$(cat $(PID_DIR)/router.pid)"
	@echo "    server  (chat)   -> :8005  PID: $$(cat $(PID_DIR)/server.pid)"
	@echo "    gateway (ws)     -> :8000  PID: $$(cat $(PID_DIR)/gateway.pid)"
	@echo ""
	@echo "==> 使用 'make status' 查看运行状态, 'make stop-all' 停止全部"

# ==================== 停止单个服务 ====================

## stop-gateway: 停止 gateway 服务
stop-gateway:
	@if [ -f $(PID_DIR)/gateway.pid ]; then \
		pid=$$(cat $(PID_DIR)/gateway.pid); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			echo "==> gateway (PID: $$pid) 已停止"; \
		else \
			echo "==> gateway 进程 $$pid 已不存在"; \
		fi; \
		rm -f $(PID_DIR)/gateway.pid; \
	else \
		echo "==> 未找到 gateway 的 PID 文件"; \
	fi

## stop-server: 停止 server 服务
stop-server:
	@if [ -f $(PID_DIR)/server.pid ]; then \
		pid=$$(cat $(PID_DIR)/server.pid); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			echo "==> server (PID: $$pid) 已停止"; \
		else \
			echo "==> server 进程 $$pid 已不存在"; \
		fi; \
		rm -f $(PID_DIR)/server.pid; \
	else \
		echo "==> 未找到 server 的 PID 文件"; \
	fi

## stop-service: 停止 service 服务
stop-service:
	@if [ -f $(PID_DIR)/service.pid ]; then \
		pid=$$(cat $(PID_DIR)/service.pid); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			echo "==> service (PID: $$pid) 已停止"; \
		else \
			echo "==> service 进程 $$pid 已不存在"; \
		fi; \
		rm -f $(PID_DIR)/service.pid; \
	else \
		echo "==> 未找到 service 的 PID 文件"; \
	fi

## stop-router: 停止 router 服务
stop-router:
	@if [ -f $(PID_DIR)/router.pid ]; then \
		pid=$$(cat $(PID_DIR)/router.pid); \
		if kill -0 $$pid 2>/dev/null; then \
			kill $$pid; \
			echo "==> router (PID: $$pid) 已停止"; \
		else \
			echo "==> router 进程 $$pid 已不存在"; \
		fi; \
		rm -f $(PID_DIR)/router.pid; \
	else \
		echo "==> 未找到 router 的 PID 文件"; \
	fi

## stop-all: 停止全部服务
stop-all: stop-gateway stop-server stop-service stop-router
	@echo "==> 全部服务已停止"

# ==================== 状态查看 ====================

## status: 查看各服务运行状态
status:
	@echo "==> 服务运行状态:"
	@for svc in gateway server service router; do \
		if [ -f $(PID_DIR)/$$svc.pid ]; then \
			pid=$$(cat $(PID_DIR)/$$svc.pid); \
			if kill -0 $$pid 2>/dev/null; then \
				printf "    %-10s RUNNING  PID: %s\n" "$$svc" "$$pid"; \
			else \
				printf "    %-10s DEAD     PID: %s (进程已退出)\n" "$$svc" "$$pid"; \
			fi; \
		else \
			printf "    %-10s STOPPED  (无 PID 文件)\n" "$$svc"; \
		fi; \
	done

# ==================== 日志查看 ====================

## logs-gateway: 查看 gateway 日志
logs-gateway:
	@tail -f $(LOG_DIR)/gateway.log

## logs-server: 查看 server 日志
logs-server:
	@tail -f $(LOG_DIR)/server.log

## logs-service: 查看 service 日志
logs-service:
	@tail -f $(LOG_DIR)/service.log

## logs-router: 查看 router 日志
logs-router:
	@tail -f $(LOG_DIR)/router.log

# ==================== 代码质量 ====================

## fmt: 格式化代码
fmt:
	@echo "==> 格式化代码..."
	$(GO) fmt ./...

## vet: 静态检查
vet:
	@echo "==> 静态检查..."
	$(GO) vet ./...

## test: 运行测试
test:
	@echo "==> 运行测试..."
	$(GO) test -v ./...

# ==================== Docker 依赖 ====================

## docker-up: 启动 MySQL/Redis/Consul 依赖容器
docker-up:
	@echo "==> 启动依赖容器 (MySQL/Redis/Consul)..."
	docker-compose -f docker-compose.yml up -d
	@echo "==> 依赖容器已启动"
	@echo "    MySQL:  localhost:3306  (root/123456)"
	@echo "    Redis:  localhost:6379"
	@echo "    Consul: localhost:8500  (UI: http://localhost:8500)"

## docker-down: 停止依赖容器
docker-down:
	@echo "==> 停止依赖容器..."
	docker-compose -f docker-compose.yml down
	@echo "==> 依赖容器已停止"

# ==================== 清理 ====================

## clean: 清理构建产物和运行时文件
clean: stop-all
	@echo "==> 清理构建产物..."
	@rm -rf $(BUILD_DIR) $(PID_DIR)
	@echo "==> 清理完成"

## clean-all: 清理所有生成文件（含日志）
clean-all: clean
	@rm -rf $(LOG_DIR)
	@echo "==> 全部清理完成"

# ==================== 帮助 ====================

## help: 显示帮助信息
help:
	@echo "Kim IM 项目 Makefile"
	@echo ""
	@echo "用法: make <target>"
	@echo ""
	@echo "【构建】"
	@echo "  build              编译主二进制到 bin/kim"
	@echo "  build-all          编译并准备运行目录"
	@echo "  deps               下载依赖"
	@echo "  tidy               整理 go.mod"
	@echo ""
	@echo "【启动单个服务（后台）】"
	@echo "  run-gateway        启动 gateway 网关服务 (:8000)"
	@echo "  run-server         启动 server 消息服务 (:8005)"
	@echo "  run-service        启动 service 数据服务 (:8080)"
	@echo "  run-router         启动 router 路由服务 (:8100)"
	@echo ""
	@echo "【启动单个服务（前台调试）】"
	@echo "  run-gateway-fg     前台启动 gateway"
	@echo "  run-server-fg      前台启动 server"
	@echo "  run-service-fg     前台启动 service"
	@echo "  run-router-fg      前台启动 router"
	@echo ""
	@echo "【批量启动/停止】"
	@echo "  run-all            后台启动全部 4 个服务"
	@echo "  stop-gateway       停止 gateway"
	@echo "  stop-server        停止 server"
	@echo "  stop-service       停止 service"
	@echo "  stop-router        停止 router"
	@echo "  stop-all           停止全部服务"
	@echo ""
	@echo "【监控】"
	@echo "  status             查看各服务运行状态"
	@echo "  logs-gateway       查看 gateway 日志"
	@echo "  logs-server        查看 server 日志"
	@echo "  logs-service       查看 service 日志"
	@echo "  logs-router        查看 router 日志"
	@echo ""
	@echo "【代码质量】"
	@echo "  fmt                格式化代码"
	@echo "  vet                静态检查"
	@echo "  test               运行测试"
	@echo ""
	@echo "【Docker 依赖】"
	@echo "  docker-up          启动 MySQL/Redis/Consul 容器"
	@echo "  docker-down        停止依赖容器"
	@echo ""
	@echo "【清理】"
	@echo "  clean              清理构建产物（会先停止服务）"
	@echo "  clean-all          清理构建产物和日志"
	@echo ""
	@echo "【典型流程】"
	@echo "  1. make docker-up          # 启动 MySQL/Redis/Consul"
	@echo "  2. make run-all            # 启动全部 kim 服务"
	@echo "  3. make status             # 查看运行状态"
	@echo "  4. make logs-gateway       # 查看日志"
	@echo "  5. make stop-all           # 停止全部服务"
	@echo "  6. make docker-down        # 停止依赖容器"
