package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testKafkaConfig struct {
	Kafka struct {
		Brokers []string `mapstructure:"brokers"`
	} `mapstructure:"kafka"`
}

func TestDecodeSliceEnv_KafkaBrokers(t *testing.T) {
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "conf.yaml")

	yamlContent := `
kafka:
  brokers:
    - "default:9092"
`
	err := os.WriteFile(confPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	t.Run("env var overrides YAML", func(t *testing.T) {
		os.Setenv("KIM_KAFKA_BROKERS", "h1:9092 h2:9092 h3:9092")
		defer os.Unsetenv("KIM_KAFKA_BROKERS")

		var cfg testKafkaConfig
		err := Load(confPath, &cfg)
		require.NoError(t, err)
		assert.Equal(t, []string{"h1:9092", "h2:9092", "h3:9092"}, cfg.Kafka.Brokers)
	})

	t.Run("no env var uses YAML value", func(t *testing.T) {
		os.Unsetenv("KIM_KAFKA_BROKERS")

		var cfg testKafkaConfig
		err := Load(confPath, &cfg)
		require.NoError(t, err)
		assert.Equal(t, []string{"default:9092"}, cfg.Kafka.Brokers)
	})

	t.Run("empty env var does not override YAML", func(t *testing.T) {
		os.Setenv("KIM_KAFKA_BROKERS", "")
		defer os.Unsetenv("KIM_KAFKA_BROKERS")

		var cfg testKafkaConfig
		err := Load(confPath, &cfg)
		require.NoError(t, err)
		assert.Equal(t, []string{"default:9092"}, cfg.Kafka.Brokers)
	})
}
