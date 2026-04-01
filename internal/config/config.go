package config

import (
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server" json:"server"`
	Log     LogConfig     `mapstructure:"log" json:"log"`
	Gateway GatewayConfig `mapstructure:"gateway" json:"gateway"`
}

type ServerConfig struct {
	Address string `mapstructure:"address" json:"address"`
}

type LogConfig struct {
	Level string `mapstructure:"level" json:"level"`
}

type GatewayConfig struct {
	DefaultModel string `mapstructure:"default_model" json:"default_model"`
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
	v.SetDefault("gateway.default_model", "gpt-4o-mini")
}
