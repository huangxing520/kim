// 文件：config.go
// 职责：Router 服务配置——通过 Viper + envconfig 加载 Router 配置。
//
// 定义的类型：
//   - Config 结构体：Router 配置（Listen / ConsulURL / LogLevel / Kafka）
//
// 方法：
//   - Init(file)       → 从 YAML + 环境变量加载配置
//   - (Config).String() → JSON 序列化

package conf

import (
	"encoding/json"
	"fmt"

	"github.com/kelseyhightower/envconfig"
	"github.com/klintcheng/kim/model"
	"github.com/spf13/viper"
)

// Config Router 服务配置
type Config struct {
	Listen    string `default:":8100"`
	ConsulURL string `default:"localhost:8500"`
	LogLevel  string `default:"INFO"`
	Kafka     model.KafkaSettings
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

	return &config, nil
}
