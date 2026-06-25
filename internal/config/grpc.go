package config

type GRPCConfig struct {
	TLSEnable   bool   `yaml:"tls_enable"`
	TLSCertFile string `yaml:"tls_cert_file"`
	TLSKeyFile  string `yaml:"tls_key_file"`
	TLSCAFile   string `yaml:"tls_ca_file"`
	AuthEnable  bool   `yaml:"auth_enable"`
	Reflection  bool   `yaml:"reflection"`
}

func DefaultGRPCConfig() GRPCConfig {
	return GRPCConfig{
		TLSEnable:  false,
		AuthEnable: false,
		Reflection: false,
	}
}
