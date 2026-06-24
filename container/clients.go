// 文件：clients.go
// 职责：管理到依赖服务的客户端连接集合——基于 sync.Map 的线程安全 ClientMap 实现。
//
// 定义的类型：
//   - ClientMap 接口：客户端连接集合的抽象（Add / Remove / Get / Services）
//   - ClientsImpl 结构体：sync.Map 包装，并发安全地管理 kim.Client 实例
//
// 方法：
//   - NewClients()                          → 创建一个新的并发安全客户端集合
//   - (ClientsImpl).Add(client)             → 添加一个客户端（以 client.ServiceID() 为 key）
//   - (ClientsImpl).Remove(id)              → 移除指定 serviceID 的客户端
//   - (ClientsImpl).Get(id)                 → 按 serviceID 获取客户端
//   - (ClientsImpl).Services(kvs...)        → 返回全部服务列表；传可选 key=value 对可按 Meta 过滤

package container

import (
	"sync"

	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
)

// ClientMap 客户端连接集合接口
type ClientMap interface {
	Add(client kim.Client)
	Remove(id string)
	Get(id string) (client kim.Client, ok bool)
	// Find(name string) (client []kim.Client)
	Services(kvs ...string) []kim.Service
}

type ClientsImpl struct {
	clients *sync.Map
}

// NewClients 创建一个 ClientMap 实例
func NewClients() ClientMap {
	return &ClientsImpl{
		clients: new(sync.Map),
	}
}

// Add addChannel
func (ch *ClientsImpl) Add(client kim.Client) {
	if client.ServiceID() == "" {
		logger.CommonLogger.WithFields(logger.Fields{
			"module": "ClientsImpl",
		}).Error("client id is required")
	}
	ch.clients.Store(client.ServiceID(), client)
}

// Remove addChannel
func (ch *ClientsImpl) Remove(id string) {
	ch.clients.Delete(id)
}

// Get Get
func (ch *ClientsImpl) Get(id string) (kim.Client, bool) {
	if id == "" {
		logger.CommonLogger.WithFields(logger.Fields{
			"module": "ClientsImpl",
		}).Error("client id is required")
	}

	val, ok := ch.clients.Load(id)
	if !ok {
		return nil, false
	}
	return val.(kim.Client), true
}

// 返回服务列表，可以传一对
func (ch *ClientsImpl) Services(kvs ...string) []kim.Service {
	kvLen := len(kvs)
	if kvLen != 0 && kvLen != 2 {
		return nil
	}
	arr := make([]kim.Service, 0)
	ch.clients.Range(func(key, val interface{}) bool {
		ser := val.(kim.Service)
		if kvLen > 0 && ser.GetMeta()[kvs[0]] != kvs[1] {
			return true
		}
		arr = append(arr, ser)
		return true
	})
	return arr
}
