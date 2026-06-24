package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Load 加载配置文件 + 环境变量覆盖
// 环境变量格式：KIM_<UPPER_FIELD>，如 KIM_CONSUL_URL
func Load(path string, out interface{}) error {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("KIM")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return fmt.Errorf("load config %s: %w", path, err)
	}
	if err := v.Unmarshal(out); err != nil {
		return fmt.Errorf("unmarshal config: %w", err)
	}
	return nil
}
