package logger

import (
	"encoding/json"
	"fmt"

	"github.com/klintcheng/kim/model"
	"github.com/spf13/viper"
)

type Server struct {
}

// Config Config
type Config struct {
	Kafka           model.KafkaSettings
	LogLevel        string `default:"DEBUG"`
	Filename        string `default:"./data/common.log"`
	Development     bool   `default:"false"`
	}

func (c Config) String() string {
	bts, _ := json.Marshal(c)
	return string(bts)
}

// InitConfig InitConfig
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
