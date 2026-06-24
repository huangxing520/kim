// 文件：redis.go
// 职责：Redis 工具函数——提供消息已读索引的 Redis key 生成、Redis 连接初始化。
//
// 方法：
//   - KeyMessageAckIndex(account)                           → 生成已读消息索引的 Redis key（chat:ack:{account}）
//   - InitRedis(addr, pass)                                 → 初始化单机 Redis 客户端
//   - InitFailoverRedis(master, sentinelAddrs, pass, timeout) → 初始化 Sentinel 哨兵模式 Redis 客户端

package database

import (
	"fmt"
	"log"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/klintcheng/kim/logger"
)

// KeyMessageAckIndex 生成已读消息索引的 Redis key
func KeyMessageAckIndex(account string) string {
	return fmt.Sprintf("chat:ack:%s", account)
}

// InitRedis return a redis instance
func InitRedis(addr string, pass string) (*redis.Client, error) {
	redisdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     pass,
		DialTimeout:  time.Second * 5,
		ReadTimeout:  time.Second * 5,
		WriteTimeout: time.Second * 5,
	})

	_, err := redisdb.Ping().Result()
	if err != nil {
		log.Println(err)
		return nil, err
	}
	return redisdb, nil
}

// InitFailoverRedis init redis with sentinels
func InitFailoverRedis(masterName string, sentinelAddrs []string, password string, timeout time.Duration) (*redis.Client, error) {
	redisdb := redis.NewFailoverClient(&redis.FailoverOptions{
		MasterName:    masterName,
		SentinelAddrs: sentinelAddrs,
		Password:      password,
		DialTimeout:   time.Second * 5,
		ReadTimeout:   timeout,
		WriteTimeout:  timeout,
	})

	_, err := redisdb.Ping().Result()
	if err != nil {
		logger.LogicLogger.Warn(err)
	}
	return redisdb, nil
}
