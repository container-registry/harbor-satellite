package config

import (
	"fmt"

	"github.com/spf13/viper"
)

type Config struct {
	LogLevel        string
	OwnRegistry     bool
	OwnRegistryAdr  string
	OwnRegistryPort string
	ZotConfigPath   string
	Input           string
}

func LoadConfig() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("error reading config file: %w", err)
	}

	return &Config{
		LogLevel:        viper.GetString("log_level"),
		OwnRegistry:     viper.GetBool("bring_own_registry"),
		OwnRegistryAdr:  viper.GetString("own_registry_adr"),
		OwnRegistryPort: viper.GetString("own_registry_port"),
		ZotConfigPath:   viper.GetString("zotConfigPath"),
		Input:           viper.GetString("url_or_file"),
	}, nil
}
