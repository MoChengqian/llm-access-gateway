package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server" json:"server"`
	Log      LogConfig      `mapstructure:"log" json:"log"`
	MySQL    MySQLConfig    `mapstructure:"mysql" json:"mysql"`
	Redis    RedisConfig    `mapstructure:"redis" json:"redis"`
	Gateway  GatewayConfig  `mapstructure:"gateway" json:"gateway"`
	Provider ProviderConfig `mapstructure:"provider" json:"provider"`
}

type ServerConfig struct {
	Address string `mapstructure:"address" json:"address"`
}

type LogConfig struct {
	Level string `mapstructure:"level" json:"level"`
}

type MySQLConfig struct {
	DSN string `mapstructure:"dsn" json:"dsn"`
}

type RedisConfig struct {
	Address  string `mapstructure:"address" json:"address"`
	Password string `mapstructure:"password" json:"password"`
	DB       int    `mapstructure:"db" json:"db"`
}

type GatewayConfig struct {
	DefaultModel             string `mapstructure:"default_model" json:"default_model"`
	ProviderFailureThreshold int    `mapstructure:"provider_failure_threshold" json:"provider_failure_threshold"`
	ProviderCooldownSeconds  int    `mapstructure:"provider_cooldown_seconds" json:"provider_cooldown_seconds"`
	PrimaryMockFailCreate    bool   `mapstructure:"primary_mock_fail_create" json:"primary_mock_fail_create"`
	PrimaryMockFailStream    bool   `mapstructure:"primary_mock_fail_stream" json:"primary_mock_fail_stream"`
}

type ProviderConfig struct {
	Primary   ProviderEndpointConfig `mapstructure:"primary" json:"primary"`
	Secondary ProviderEndpointConfig `mapstructure:"secondary" json:"secondary"`
}

type ProviderEndpointConfig struct {
	Type    string `mapstructure:"type" json:"type"`
	Name    string `mapstructure:"name" json:"name"`
	BaseURL string `mapstructure:"base_url" json:"base_url"`
	APIKey  string `mapstructure:"api_key" json:"api_key"`
	Model   string `mapstructure:"model" json:"model"`
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath("configs")
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, err
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("server.address", ":8080")
	v.SetDefault("log.level", "info")
	v.SetDefault("mysql.dsn", "")
	v.SetDefault("redis.address", "")
	v.SetDefault("redis.password", "")
	v.SetDefault("redis.db", 0)
	v.SetDefault("gateway.default_model", "gpt-4o-mini")
	v.SetDefault("gateway.provider_failure_threshold", 1)
	v.SetDefault("gateway.provider_cooldown_seconds", 30)
	v.SetDefault("gateway.primary_mock_fail_create", false)
	v.SetDefault("gateway.primary_mock_fail_stream", false)
	v.SetDefault("provider.primary.type", "mock")
	v.SetDefault("provider.primary.name", "primary")
	v.SetDefault("provider.primary.base_url", "")
	v.SetDefault("provider.primary.api_key", "")
	v.SetDefault("provider.primary.model", "")
	v.SetDefault("provider.secondary.type", "mock")
	v.SetDefault("provider.secondary.name", "secondary")
	v.SetDefault("provider.secondary.base_url", "")
	v.SetDefault("provider.secondary.api_key", "")
	v.SetDefault("provider.secondary.model", "")
}
