# model 模块

## 模块概述
公共数据模型定义模块，当前提供 Kafka 日志配置结构体，供 logger 等模块引用。

## 架构设计
模块为纯数据结构定义包，无逻辑代码，仅定义跨模块共享的配置结构体。

## 关键组件

| 组件 | 类型 | 作用 |
|------|------|------|
| `KafkaSettings` | 结构体 | Kafka 日志写入配置 |

## 核心接口

```go
type KafkaSettings struct {
    Enable            bool
    Brokers           []string
    Topic             string
    BufferSize        int
    Timeout           string
    ReplicationFactor int
    Partitions        int
}
```

### KafkaSettings 字段说明
| 字段 | 类型 | 说明 |
|------|------|------|
| Enable | bool | 是否启用 Kafka 日志写入 |
| Brokers | []string | Kafka broker 地址列表 |
| Topic | string | 日志发送的 topic |
| BufferSize | int | 生产者缓冲区大小 |
| Timeout | string | 超时时间（字符串格式，如 "5s"） |
| ReplicationFactor | int | Topic 副本因子 |
| Partitions | int | Topic 分区数 |

## 使用示例

```go
import "github.com/klintcheng/kim/model"

cfg := model.KafkaSettings{
    Enable:     true,
    Brokers:    []string{"127.0.0.1:9092"},
    Topic:      "kim-log",
    BufferSize: 1024,
}
```

## 依赖关系
本模块无第三方依赖，仅使用标准库。

### 被依赖关系
- `internal/logger` - Settings 结构体中嵌入 KafkaSettings 用于配置 Kafka 日志写入
