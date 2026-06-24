// 文件：config.go
// 职责：日志配置加载——通过 Viper 从 YAML 配置文件读取日志配置。
//
// 定义的类型：
//   - Config 结构体：日志配置（Kafka、日志等级、文件名、开发模式）
//   - Server 结构体：空结构体（预留用途）
//
// 方法：
//   - InitConfig(file) → 从指定 YAML 文件加载 Config
//   - (Config).String() → JSON 序列化配置（用于调试打印）

package logger

import (
	"encoding/json"
	"fmt"

	"github.com/klintcheng/kim/model"
	"github.com/spf13/viper"
)

// Server 空结构体（预留）
type Server struct {
}

// Config 日志配置
type Config struct {
	Kafka       model.KafkaSettings
	LogLevel    string `default:"DEBUG"`
	Filename    string `default:"./data/common.log"`
	Development bool   `default:"false"`
}

// String JSON 序列化配置
func (c Config) String() string {
	bts, _ := json.Marshal(c)
	return string(bts)
}

// InitConfig 从 YAML 文件加载日志配置
func InitConfig(file string) (*Config, error) {
	viper.SetConfigFile(file)
	viper.AddConfigPath(".")
	viper.AddConfigPath("/etc/conf")

	var config Config

	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
	} else {
		if err := viper.Unmarshal(&config); err != nil {
			return nil, err
		}
	}
	return &config, nil
}
