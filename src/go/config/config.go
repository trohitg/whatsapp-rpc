package config

import (
	"github.com/spf13/viper"
)

type Config struct {
	Environment string           `mapstructure:"environment"`
	LogLevel    int              `mapstructure:"log_level"`
	Server      ServerConfig     `mapstructure:"server"`
	Database    DatabaseConfig   `mapstructure:"database"`
	Newsletter  NewsletterConfig `mapstructure:"newsletter"`
	QRTimeout   int              `mapstructure:"qr_timeout_seconds"`
}

type ServerConfig struct {
	Port int    `mapstructure:"port"`
	Host string `mapstructure:"host"`
}

type DatabaseConfig struct {
	Path string `mapstructure:"path"`
}

type NewsletterConfig struct {
	FetchPageSize  int `mapstructure:"fetch_page_size"`
	FetchDelayMs   int `mapstructure:"fetch_delay_ms"`
	DefaultLimit   int `mapstructure:"default_limit"`
	MaxLimit       int `mapstructure:"max_limit"`
	MediaCacheSize int `mapstructure:"media_cache_size"`
}

func Load() (*Config, error) {
	viper.SetDefault("environment", "development")
	viper.SetDefault("log_level", 4) // Info level
	viper.SetDefault("server.port", 9400)
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("database.path", "data/whatsapp.db")
	viper.SetDefault("qr_timeout_seconds", 300)
	viper.SetDefault("newsletter.fetch_page_size", 100)
	viper.SetDefault("newsletter.fetch_delay_ms", 2000)
	viper.SetDefault("newsletter.default_limit", 50)
	viper.SetDefault("newsletter.max_limit", 500)
	viper.SetDefault("newsletter.media_cache_size", 100)

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