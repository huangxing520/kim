// 文件：selector.go
// 职责：定义服务节点选择器接口及哈希辅助函数——消息路由时从多个服务节点中选出一个目标。
//
// 定义的类型：
//   - Selector 接口：路由选择器抽象，Lookup 根据消息头从候选服务列表中选出一个 serviceID
//
// 方法：
//   - HashCode(key) → 对字符串生成 CRC32 哈希值（用于一致性哈希路由的基础工具函数）

package container

import (
	"hash/crc32"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/wire/pkt"
)

// HashCode 对字符串 key 生成 CRC32 哈希值
func HashCode(key string) int {
	hash32 := crc32.NewIEEE()
	hash32.Write([]byte(key))
	return int(hash32.Sum32())
}

// Selector 路由选择器接口，根据消息头从候选 service 列表中选择一个 serviceID
type Selector interface {
	Lookup(*pkt.Header, []kim.Service) string
}
