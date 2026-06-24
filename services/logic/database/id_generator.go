// 文件：id_generator.go
// 职责：分布式 ID 生成器——基于 Twitter Snowflake 算法的全局唯一 ID 生成（用于消息 ID / 群组 ID 等）。
//
// 定义的类型：
//   - IDGenerator 结构体：Snowflake ID 生成器包装
//
// 方法：
//   - NewIDGenerator(nodeID)         → 以指定 nodeID 创建生成器实例
//   - (IDGenerator).Next()           → 生成下一个唯一 ID
//   - (IDGenerator).ParseBase36(id)  → 将 Base36 字符串解析为 ID
//   - (IDGenerator).Parse(id)        → 将 int64 解析为 ID

package database

import "github.com/bwmarrin/snowflake"

// IDGenerator Snowflake ID 生成器
type IDGenerator struct {
	node *snowflake.Node
}

// NewIDGenerator 创建 IDGenerator
func NewIDGenerator(nodeID int64) (*IDGenerator, error) {
	node, err := snowflake.NewNode(nodeID)
	if err != nil {
		return nil, err
	}
	return &IDGenerator{node: node}, nil
}

// Next 生成下一个唯一 ID
func (g *IDGenerator) Next() snowflake.ID {
	return g.node.Generate()
}

// ParseBase36 将 Base36 字符串解析为 Snowflake ID
func (g *IDGenerator) ParseBase36(id string) (snowflake.ID, error) {
	return snowflake.ParseBase36(id)
}

// Parse 将 int64 解析为 Snowflake ID
func (g *IDGenerator) Parse(id int64) snowflake.ID {
	return snowflake.ParseInt64(id)
}
