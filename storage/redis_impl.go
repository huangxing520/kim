// 文件：redis_impl.go
// 职责：Redis 会话存储实现——SessionStorage 接口的 Redis 实现，使用 Pipeline 优化批量写入。
//
// 常量：
//   - LocationExpired：位置信息和 Session 的过期时间（48 小时）
//
// 定义的类型：
//   - RedisStorage 结构体：基于 Redis 的 SessionStorage 实现
//
// 方法：
//   - NewRedisStorage(cli)                        → 创建 RedisStorage
//   - (RedisStorage).Add(session)                 → Pipeline 写入 location + session
//   - (RedisStorage).Delete(account, channelId)    → Pipeline 删除 location + session
//   - (RedisStorage).Get(channelId)                → 按 channelId 获取 Session
//   - (RedisStorage).GetLocations(accounts...)     → MGet 批量查询用户位置（保持顺序，不在线的返回 nil）
//   - (RedisStorage).GetLocation(account, device)  → 查询单个用户位置
//   - (RedisStorage).RedisGet(key)                 → 通用 Redis GET

package storage

import (
	"fmt"
	"time"

	"github.com/go-redis/redis/v7"
	kim "github.com/klintcheng/kim/internal/kim"
	"github.com/klintcheng/kim/wire/pkt"
	"google.golang.org/protobuf/proto"
)

// LocationExpired 位置和 Session 过期时间
const (
	LocationExpired = time.Hour * 48
)

// RedisStorage SessionStorage 的 Redis 实现
type RedisStorage struct {
	cli *redis.Client
}

func NewRedisStorage(cli *redis.Client) kim.SessionStorage {
	return &RedisStorage{
		cli: cli,
	}
}

func (r *RedisStorage) Add(session *pkt.Session) error {
	// save kim.Location
	loc := kim.Location{
		ChannelId: session.ChannelId,
		GateId:    session.GateId,
	}
	locKey := KeyLocation(session.Account, "")
	snKey := KeySession(session.ChannelId)
	buf, _ := proto.Marshal(session)
	// 【修复#12】原代码两次独立的 Set 产生两次网络往返（2次 RTT）
	// 登录是高频操作，应合并为一次 Pipeline 请求
	// 新加的：使用 Pipeline 将两个 Set 合并为一次网络往返
	pipe := r.cli.Pipeline()                       // 新加的：创建 Pipeline
	pipe.Set(locKey, loc.Bytes(), LocationExpired) // 新加的：批量设置 location
	pipe.Set(snKey, buf, LocationExpired)          // 新加的：批量设置 session
	_, err := pipe.Exec()                          // 新加的：一次性执行，仅 1 次 RTT
	if err != nil {
		return err
	}
	return nil
}

// Delete a session
func (r *RedisStorage) Delete(account string, channelId string) error {
	locKey := KeyLocation(account, "")
	snKey := KeySession(channelId)
	// 【修复#12】原代码两次独立的 Del 产生两次网络往返
	// 新加的：使用 Pipeline 将两个 Del 合并为一次网络往返
	pipe := r.cli.Pipeline() // 新加的：创建 Pipeline
	pipe.Del(locKey)         // 新加的：批量删除 location
	pipe.Del(snKey)          // 新加的：批量删除 session
	_, err := pipe.Exec()    // 新加的：一次性执行，仅 1 次 RTT
	if err != nil {
		return err
	}
	return nil
}

// GetByID get session by sessionID
func (r *RedisStorage) Get(channelId string) (*pkt.Session, error) {
	snKey := KeySession(channelId)
	bts, err := r.cli.Get(snKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var session pkt.Session
	_ = proto.Unmarshal(bts, &session)
	return &session, nil
}

func (r *RedisStorage) GetLocations(accounts ...string) ([]*kim.Location, error) {
	keys := KeyLocations(accounts...)
	list, err := r.cli.MGet(keys...).Result()
	if err != nil {
		return nil, err
	}
	// 【修复#13】原代码遍历 list 时跳过 nil 元素，导致返回的 Location 列表
	// 丢失了与 accounts 的对应关系，调用方无法知道哪些账号不在线
	// 群聊推送时可能导致错推或漏推
	// 新加的：保持返回结果与 accounts 一一对应，nil 表示该账号不在线
	result := make([]*kim.Location, len(accounts)) // 新加的：预分配与 accounts 等长的切片
	for i, l := range list {
		if l == nil {
			result[i] = nil // 新加的：不在线的账号保留 nil，保持索引对应
			continue
		}
		var loc kim.Location
		_ = loc.Unmarshal([]byte(l.(string)))
		result[i] = &loc // 新加的：按原顺序填充
	}
	// 【修复#13】原代码 len(result)==0 时返回 ErrSessionNil，但修复后 result 长度等于 accounts 长度
	// 新加的：检查是否全部不在线
	allNil := true // 新加的：标记是否全部账号都不在线
	for _, loc := range result {
		if loc != nil {
			allNil = false
			break
		}
	}
	if allNil {
		return nil, kim.ErrSessionNil
	}
	return result, nil
}

func (r *RedisStorage) GetLocation(account string, device string) (*kim.Location, error) {
	key := KeyLocation(account, device)
	bts, err := r.cli.Get(key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, kim.ErrSessionNil
		}
		return nil, err
	}
	var loc kim.Location
	_ = loc.Unmarshal(bts)
	return &loc, nil
}

func (r *RedisStorage) RedisGet(key string) (string, error) {
	result, error := r.cli.Get(key).Result()
	return result, error
}

func KeySession(channel string) string {
	return fmt.Sprintf("login:sn:%s", channel)
}

func KeyLocation(account, device string) string {
	if device == "" {
		return fmt.Sprintf("login:loc:%s", account)
	}
	return fmt.Sprintf("login:loc:%s:%s", account, device)
}

func KeyLocations(accounts ...string) []string {
	arr := make([]string, len(accounts))
	for i, account := range accounts {
		arr[i] = KeyLocation(account, "")
	}
	return arr
}
