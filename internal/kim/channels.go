// 文件：channels.go
// 职责：Channel 连接管理容器——基于 sync.Map 的并发安全 Channel 集合，管理所有活跃的客户端连接。
//
// 定义的类型：
//   - ChannelMap 接口：Channel 集合的抽象（Add / Remove / Get / All）
//   - ChannelsImpl 结构体：sync.Map 包装实现
//
// 方法：
//   - NewChannels()                         → 创建一个新的并发安全 Channel 集合
//   - (ChannelsImpl).Add(channel)           → 添加一个 Channel（以 channel.ID() 为 key）
//   - (ChannelsImpl).Remove(id)             → 移除指定 ID 的 Channel
//   - (ChannelsImpl).Get(id)                → 按 ID 获取 Channel，返回 Channel 和 bool
//   - (ChannelsImpl).All()                  → 返回当前所有 Channel 的切片

package kim

import (
	"sync"

	"github.com/klintcheng/kim/internal/logger"
)

// ChannelMap Channel 集合接口
type ChannelMap interface {
	Add(channel Channel)
	Remove(id string)
	Get(id string) (channel Channel, ok bool)
	All() []Channel
}

// ChannelsImpl ChannelMap
type ChannelsImpl struct {
	channels *sync.Map
}

// NewChannels NewChannels
func NewChannels() ChannelMap {
	return &ChannelsImpl{
		channels: new(sync.Map),
	}
}

// Add addChannel
func (ch *ChannelsImpl) Add(channel Channel) {
	if channel.ID() == "" {
		logger.CommonLogger.WithFields(logger.Fields{
			"module": "ChannelsImpl",
		}).Error("channel id is required")
		return
	}

	ch.channels.Store(channel.ID(), channel)
}

// Remove addChannel
func (ch *ChannelsImpl) Remove(id string) {
	ch.channels.Delete(id)
}

// Get Get
func (ch *ChannelsImpl) Get(id string) (Channel, bool) {
	if id == "" {
		logger.CommonLogger.WithFields(logger.Fields{
			"module": "ChannelsImpl",
		}).Error("channel id is required")
		return nil, false
	}

	val, ok := ch.channels.Load(id)
	if !ok {
		return nil, false
	}
	return val.(Channel), true
}

// All return channels
func (ch *ChannelsImpl) All() []Channel {
	// 【修复#17】原代码 arr := make([]Channel, 0) 没有预分配容量
	// sync.Map 无法获取长度，但可以预估一个容量减少扩容次数
	// 新加的：预分配一个合理的初始容量，减少 append 时的多次扩容
	arr := make([]Channel, 0, 64) // 新加的：预分配 64 容量，减少扩容
	ch.channels.Range(func(key, val interface{}) bool {
		arr = append(arr, val.(Channel))
		return true
	})
	return arr
}
