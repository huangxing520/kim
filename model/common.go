// 文件：common.go
// 职责：公共模型定义——Kafka 配置结构体。
//
// 定义的类型：
//   - KafkaSettings 结构体：Kafka 日志配置（Enable / Brokers / Topic / BufferSize 等）

package model

// KafkaSettings Kafka 日志配置
type KafkaSettings struct {
	Enable            bool
	Brokers           []string
	Topic             string
	BufferSize        int
	Timeout           string
	ReplicationFactor int
	Partitions        int
}