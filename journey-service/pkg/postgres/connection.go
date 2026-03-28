package postgres

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Config holds database connection configuration
type Config struct {
	Host         string
	Port         int
	User         string
	Password     string
	DBName       string
	MaxOpenConns int
	MaxIdleConns int
}

// ConnectionPools holds master and slave database connections
type ConnectionPools struct {
	Master *sql.DB
	Slave  *sql.DB
}

// NewConnectionPools creates new master and slave database connection pools
func NewConnectionPools(masterCfg, slaveCfg Config) (*ConnectionPools, error) {
	master, err := connect(masterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to master database: %w", err)
	}

	slave, err := connect(slaveCfg)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to connect to slave database: %w", err)
	}

	return &ConnectionPools{
		Master: master,
		Slave:  slave,
	}, nil
}

// connect establishes a database connection with the given configuration
func connect(cfg Config) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DBName,
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database connection: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Hour)

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// Close closes both master and slave database connections
func (cp *ConnectionPools) Close() error {
	var masterErr, slaveErr error

	if cp.Master != nil {
		masterErr = cp.Master.Close()
	}

	if cp.Slave != nil {
		slaveErr = cp.Slave.Close()
	}

	if masterErr != nil {
		return fmt.Errorf("failed to close master connection: %w", masterErr)
	}

	if slaveErr != nil {
		return fmt.Errorf("failed to close slave connection: %w", slaveErr)
	}

	return nil
}
