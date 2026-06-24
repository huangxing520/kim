// 文件：storage.go
// 职责：会话存储接口——定义 Session 的增删查操作，以及按账号查询用户位置。
//
// 定义的类型：
//   - SessionStorage 接口：会话存储抽象，提供 Session 的 CRUD 和用户位置查询
//
// 方法（接口定义）：
//   - Add(session)                                → 添加一个 Session
//   - Delete(account, channelId)                  → 删除指定账号的指定 Channel Session
//   - Get(channelId)                              → 按 channelId 获取 Session
//   - GetLocations(accounts...)                   → 按账号列表查询用户所在位置（gateway + channel）
//   - GetLocation(account, device)                → 按账号和设备查询单个用户位置
//   - RedisGet(key)                               → 直接从 Redis 获取 key 的值

package kim

import (
	"errors"

	"github.com/klintcheng/kim/wire/pkt"
)

// ErrSessionNil Session 为空时的标准错误
var ErrSessionNil = errors.New("err:session nil")

// SessionStorage 会话存储接口，提供 Session 的增删查和用户位置查询
type SessionStorage interface {
	// Add a session
	Add(session *pkt.Session) error
	// Delete a session
	Delete(account string, channelId string) error
	// Get session by channelId
	Get(channelId string) (*pkt.Session, error)
	// Get Locations by accounts
	GetLocations(account ...string) ([]*Location, error)
	// Get Location by account and device
	GetLocation(account string, device string) (*Location, error)
	RedisGet(key string) (string, error)
}
