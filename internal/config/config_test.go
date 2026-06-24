package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// 创建临时配置文件
	tmpFile := "/tmp/test_config.yaml"
	content := `
service_id: "test-1"
listen: ":8000"
log_level: "info"
`
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile)

	var cfg struct {
		ServiceID string `mapstructure:"service_id"`
		Listen    string `mapstructure:"listen"`
		LogLevel  string `mapstructure:"log_level"`
	}

	if err := Load(tmpFile, &cfg); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.ServiceID != "test-1" {
		t.Errorf("ServiceID = %s, want test-1", cfg.ServiceID)
	}
	if cfg.Listen != ":8000" {
		t.Errorf("Listen = %s, want :8000", cfg.Listen)
	}
}

func TestLoadWithEnvOverride(t *testing.T) {
	tmpFile := "/tmp/test_config_env.yaml"
	content := `
service_id: "test-1"
listen: ":8000"
`
	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile)

	os.Setenv("KIM_LISTEN", ":9999")
	defer os.Unsetenv("KIM_LISTEN")

	var cfg struct {
		ServiceID string `mapstructure:"service_id"`
		Listen    string `mapstructure:"listen"`
	}

	if err := Load(tmpFile, &cfg); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Listen != ":9999" {
		t.Errorf("Listen = %s, want :9999 (env override)", cfg.Listen)
	}
}
