package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Environment string         `mapstructure:"environment"`
	LogLevel    int            `mapstructure:"log_level"`
	Server      ServerConfig   `mapstructure:"server"`
	Database    DatabaseConfig `mapstructure:"database"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

func Load() (*Config, error) {
	viper.SetDefault("environment", "development")
	viper.SetDefault("log_level", 4) // Info level
	viper.SetDefault("server.port", 9400)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("database.path", "data/whatsapp.db")

	// Environment variables
	viper.SetEnvPrefix("WA")
	viper.AutomaticEnv()

	// Config file
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./configs")

	// Read config file (optional)
	viper.ReadInConfig()

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	return &config, nil
}