package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Redis    RedisConfig    `mapstructure:"redis"`
	IAM      IAMConfig      `mapstructure:"iam"`
	Logging  LoggingConfig  `mapstructure:"logging"`
}

type ServerConfig struct {
	Port    int           `mapstructure:"port"`
	Timeout time.Duration `mapstructure:"timeout"`
}

type DatabaseConfig struct {
	Master DBConfig `mapstructure:"master"`
	Slave  DBConfig `mapstructure:"slave"`
}

type DBConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"dbname"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type IAMConfig struct {
	PrivateKeyPath     string        `mapstructure:"private_key_path"`
	SigningKID         string        `mapstructure:"signing_kid"`
	PreviousKID        string        `mapstructure:"previous_kid"`
	PreviousPubKeyPEM  string        `mapstructure:"previous_pub_key_pem"`
	AccessTokenTTL     time.Duration `mapstructure:"access_token_ttl"`
	RefreshTokenTTL    time.Duration `mapstructure:"refresh_token_ttl"`
	BcryptCost         int           `mapstructure:"bcrypt_cost"`
	TokenRetentionDays int           `mapstructure:"token_retention_days"`
	CleanupInterval    time.Duration `mapstructure:"cleanup_interval"`
}

type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

func Load(configPath string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	v.SetEnvPrefix("VCS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	// AutomaticEnv does not reliably override nested keys during Unmarshal.
	// BindEnv forces viper to always check the env var for these keys.
	for _, key := range []string{
		"database.master.host", "database.master.port", "database.master.user",
		"database.master.password", "database.master.dbname",
		"database.slave.host", "database.slave.port", "database.slave.user",
		"database.slave.password", "database.slave.dbname",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}
	if cfg.IAM.AccessTokenTTL == 0 {
		cfg.IAM.AccessTokenTTL = 24 * time.Hour
	}
	if cfg.IAM.RefreshTokenTTL == 0 {
		cfg.IAM.RefreshTokenTTL = 7 * 24 * time.Hour
	}
	if cfg.IAM.BcryptCost == 0 {
		cfg.IAM.BcryptCost = 12
	}
	if cfg.IAM.TokenRetentionDays == 0 {
		cfg.IAM.TokenRetentionDays = 7
	}
	if cfg.IAM.CleanupInterval == 0 {
		cfg.IAM.CleanupInterval = 24 * time.Hour
	}
	if cfg.IAM.SigningKID == "" {
		cfg.IAM.SigningKID = "key-dev-001"
	}
	return &cfg, nil
}
