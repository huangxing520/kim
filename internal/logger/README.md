# logger 模块

## 模块概述
基于 Zap 的日志封装模块，支持按等级分文件滚动输出、控制台彩色输出、Kafka 异步写入，提供兼容 logrus 风格的 SugaredLogger 链式调用 API。

## 架构设计
日志模块采用多 Core 组合的架构：不同日志等级（Debug/Info/Warn/Error）分别写入独立文件，开发模式下额外输出到控制台（带颜色），可选通过异步 Kafka 生产者将 Info 及以上日志发送到 Kafka。包初始化时通过 `init()` 函数以默认配置初始化 `CommonLogger`（容错处理，失败静默返回），各服务在启动阶段需调用 `Init` 创建自身专用 Logger 并赋值给对应的全局变量。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `Logger` | 结构体 | 封装 `*zap.Logger`，管理多输出目标、文件滚动、Kafka Writer |
| `SugaredLogger` | 结构体 | 包装 `*zap.SugaredLogger`，补全 `Trace` 方法，提供链式调用 |
| `Options` | 结构体 | 函数式选项模式配置结构 |
| `Settings` | 结构体 | 旧 API 兼容配置结构 |
| `KafkaWriter` | 结构体 | Kafka 异步生产者 `io.Writer` 适配 |
| `Fields` | 类型 | `map[string]interface{}`，兼容 logrus 风格字段 |

### 全局 Logger 变量
| 变量 | 用途 |
|------|------|
| `CommonLogger` | 通用日志（init() 默认初始化） |
| `GatewayLogger` | Gateway 服务专用日志 |
| `LogicLogger` | Logic 服务专用日志 |
| `CometLogger` | Comet 服务专用日志 |
| `RouterLogger` | Router 服务专用日志 |

## 核心接口

```go
// Init 使用旧 API 创建 Logger 实例（推荐服务启动时使用）
func Init(s Settings) (*Logger, error)

// NewLogger 使用函数式选项创建 Logger 实例
func NewLogger(mod ...ModOptions) *Logger

// Sugar 返回 SugaredLogger 用于链式调用
func (l *Logger) Sugar() *SugaredLogger

// Close 关闭日志（同步 zap + 关闭 Kafka 生产者）
func (l *Logger) Close() error

// SugaredLogger 链式方法
func (s *SugaredLogger) Trace(args ...interface{})
func (s *SugaredLogger) Tracef(template string, args ...interface{})
func (s *SugaredLogger) WithField(key string, val interface{}) *SugaredLogger
func (s *SugaredLogger) WithFields(fields Fields) *SugaredLogger
func (s *SugaredLogger) WithError(err error) *SugaredLogger

// 函数式选项工厂
func SetMaxSize(size int) ModOptions
func SetMaxBackups(backups int) ModOptions
func SetMaxAge(age int) ModOptions
func SetLogFileDir(dir string) ModOptions
func SetAppName(name string) ModOptions
func SetLevel(level zapcore.Level) ModOptions
func SetDevelopment(dev bool) ModOptions
func SetKafka(addrs []string, topic string) ModOptions
func SetServiceName(name string) ModOptions
```

## 配置说明

### Settings 字段
| 字段 | 类型 | 说明 |
|------|------|------|
| Filename | string | 日志文件路径 |
| Level | string | 日志等级：DEBUG/INFO/WARN/ERROR |
| RollingDays | uint | 日志保留天数 |
| Format | string | 日志格式 |
| ServiceName | string | 服务名，自动注入每条日志的 "service" 字段 |
| Development | bool | 是否开发模式（控制台输出+彩色） |
| Kafka | model.KafkaSettings | Kafka 日志写入配置 |

### 文件滚动策略（基于 lumberjack）
- 单文件最大 100MB
- 最多保留 60 个切片文件
- 日志保留 30 天
- 自动压缩旧日志
- 使用本地时间

### Kafka 写入特性
- 使用 sarama 异步生产者
- Snappy 压缩
- Channel 缓冲 1024 条
- 缓冲区满时**丢弃日志**，不阻塞业务
- 仅发送 Info 及以上级别日志到 Kafka

## 使用示例

### 服务初始化（推荐方式）
```go
log, err := logger.Init(logger.Settings{
    Level:       "info",
    Filename:    "./data/gateway.log",
    ServiceName: "gateway",
    Development: true,
    Kafka: model.KafkaSettings{
        Enable:  true,
        Brokers: []string{"127.0.0.1:9092"},
        Topic:   "kim-log",
    },
})
if err != nil {
    panic(err)
}
logger.GatewayLogger = log.Sugar()
defer log.Close()
```

### 使用 SugaredLogger 链式调用
```go
logger.GatewayLogger.
    WithField("channel_id", ch.ID()).
    WithError(err).
    Error("push message failed")

logger.CommonLogger.WithFields(logger.Fields{
    "module": "DefaultServer",
    "id":     s.ServiceID(),
}).Info("server started")
```

### 函数式选项创建 Logger
```go
log := logger.NewLogger(
    logger.SetServiceName("logic"),
    logger.SetLevel(zapcore.InfoLevel),
    logger.SetDevelopment(false),
    logger.SetLogFileDir("./logs"),
)
```

## 依赖关系
- `go.uber.org/zap` - 核心日志库
- `gopkg.in/natefinch/lumberjack.v2` - 文件滚动
- `github.com/IBM/sarama` - Kafka 客户端
- `github.com/klintcheng/kim/model` - KafkaSettings 配置
