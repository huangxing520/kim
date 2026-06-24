// 文件：config.go
// 职责：Gateway 配置加载——通过 Viper + envconfig 从 YAML 文件和环境变量加载 Gateway 服务配置。
//
// 定义的类型：
//   - Config 结构体：Gateway 配置（ServiceID / Listen / ConsulURL / AppSecret / 协程池等）
//
// 方法：
//   - Init(file)       → 加载配置（环境变量 + YAML 文件，自动生成默认 ServiceID）
//   - (Config).String() → JSON 序列化配置

package conf

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kelseyhightower/envconfig"
	"github.com/klintcheng/kim"
	"github.com/klintcheng/kim/model"
	"github.com/spf13/viper"
)

// Config Gateway 服务配置
type Config struct {
	ServiceID       string
	ServiceName     string `default:"wgateway"`
	Listen          string `default:":8000"`
	PublicAddress   string
	PublicPort      int `default:"8000"`
	Tags            []string
	Domain          string
	ConsulURL       string
	MonitorPort     int `default:"8001"`
	AppSecret       string
	LogLevel        string `default:"DEBUG"`
	MessageGPool    int    `default:"10000"`
	ConnectionGPool int    `default:"15000"`
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
		config.ServiceID = fmt.Sprintf("gate_%s", strings.ReplaceAll(localIP, ".", ""))
	}
	if config.PublicAddress == "" {
		config.PublicAddress = kim.GetLocalIP()
	}
	fmt.Println(config)
	return &config, nil
}
