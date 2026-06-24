// 文件：config.go
// 职责：Comet 配置加载——通过 Viper + envconfig 加载配置，并提供 Redis 连接初始化。
//
// 定义的类型：
//   - Config 结构体：Comet 配置（ServiceID / Listen / ConsulURL / RedisAddrs / RoyalURL / Zone 等）
//   - Server 结构体：空结构体（预留）
//
// 方法：
//   - Init(file)                          → 加载配置（环境变量 + YAML，自动生成默认 ServiceID）
//   - (Config).String()                   → JSON 序列化
//   - InitRedis(addr, pass)               → 初始化单机 Redis 客户端
//   - InitFailoverRedis(master, sentinels) → 初始化 Sentinel 哨兵模式 Redis 客户端

package conf

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/kelseyhightower/envconfig"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/logger"
	"github.com/klintcheng/kim/model"
	"github.com/spf13/viper"
)

// Server 空结构体（预留）
type Server struct {
}

// Config Comet 服务配置
type Config struct {
	ServiceID       string
	Listen          string `default:":8005"`
	MonitorPort     int    `default:"8006"`
	PublicAddress   string
	PublicPort      int `default:"8005"`
	Tags            []string
	Zone            string `default:"zone_ali_03"`
	ConsulURL       string
	RedisAddrs      string
	RoyalURL        string
	LogLevel        string `default:"DEBUG"`
	MessageGPool    int    `default:"5000"`
	ConnectionGPool int    `default:"500"`
	Kafka           model.KafkaSettings
}

func (c Config) String() string {
	bts, _ := json.Marshal(c)
	return string(bts)
}

// Init InitConfig
func Init(file string) (*Config, error) {
	viper.SetConfigFile(file)
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/conf")

	var config Config
	err := envconfig.Process("kim", &config)
	if err != nil {
		return nil, err
	}

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
	} else {
		if err := viper.Unmarshal(&config); err != nil {
			return nil, err
		}
	}

	if config.ServiceID == "" {
		localIP := kim.GetLocalIP()
		config.ServiceID = fmt.Sprintf("server_%s", strings.ReplaceAll(localIP, ".", ""))
	}
	if config.PublicAddress == "" {
		config.PublicAddress = kim.GetLocalIP()
	}
	fmt.Println(config)
	return &config, nil
}

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
		fmt.Println(err)
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
		logger.CometLogger.Warn(err)
	}
	return redisdb, nil
}
