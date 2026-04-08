package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Database DatabaseConfig `mapstructure:"database"`
	Logging  LoggingConfig  `mapstructure:"logging"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Services ServicesConfig `mapstructure:"services"`
	Business BusinessConfig `mapstructure:"business"`
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

// ServicesConfig holds URLs for co-located services
type ServicesConfig struct {
	CapacityURL string `mapstructure:"capacity_url"`
	MapURL      string `mapstructure:"map_url"`
	IAMURL      string `mapstructure:"iam_url"`
	JWTSecret   string `mapstructure:"jwt_secret"`
}

// BusinessConfig holds business rule configuration
type BusinessConfig struct {
	MinAdvanceBookingMinutes     int `mapstructure:"min_advance_booking_minutes"`
	MinCancellationWindowMinutes int `mapstructure:"min_cancellation_window_minutes"`
	ActivationGraceWindowMinutes int `mapstructure:"activation_grace_window_minutes"`
	RouteCacheTTLHours           int `mapstructure:"route_cache_ttl_hours"`
	MaxCapacityRetries           int `mapstructure:"max_capacity_retries"`
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port    int           `mapstructure:"port"`
	Timeout time.Duration `mapstructure:"timeout"`
}

// DatabaseConfig holds master and slave database configurations
type DatabaseConfig struct {
	Master DBConfig `mapstructure:"master"`
	Slave  DBConfig `mapstructure:"slave"`
}

// DBConfig holds individual database connection configuration
type DBConfig struct {
	Host         string `mapstructure:"host"`
	Port         int    `mapstructure:"port"`
	User         string `mapstructure:"user"`
	Password     string `mapstructure:"password"`
	DBName       string `mapstructure:"dbname"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

// LoggingConfig represents logging configuration
type LoggingConfig struct {
	Level  string `mapstructure:"level"`
	Format string `mapstructure:"format"`
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set config file
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")

	// Read config file
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Allow environment variable overrides
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
		"services.capacity_url", "services.map_url", "services.iam_url", "services.jwt_secret",
	} {
		_ = v.BindEnv(key)
	}

	// Unmarshal config
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &cfg, nil
}
