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

// Connect establishes a single database connection with the given configuration.
// Exported so callers (e.g. main.go building regional pools) can create
// additional pools without going through NewConnectionPools.
func Connect(cfg Config) (*sql.DB, error) {
	return connect(cfg)
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

// RegionalPools maps each geo-region ("eu", "us", "apac") to a dedicated
// CockroachDB master pool whose gateway node is co-located with that region's
// leaseholders.  When a region-specific pool is nil it falls back to the EU pool,
// which is always required (it doubles as the default).
//
// In single-cluster / single-cell deployments all three pointers can point to
// the same *sql.DB — the Saga coordinator will still use separate transactions.
type RegionalPools struct {
	EU   *sql.DB
	US   *sql.DB
	APAC *sql.DB
}

// Get returns the pool for the given region, falling back to EU if the
// region-specific pool was not configured.
func (rp *RegionalPools) Get(region string) *sql.DB {
	switch region {
	case "us":
		if rp.US != nil {
			return rp.US
		}
	case "apac":
		if rp.APAC != nil {
			return rp.APAC
		}
	}
	return rp.EU
}

// Close closes all non-nil pools that are distinct objects (avoids double-close
// when all three pointers share the same underlying *sql.DB).
func (rp *RegionalPools) Close() {
	closed := map[*sql.DB]bool{}
	for _, db := range []*sql.DB{rp.EU, rp.US, rp.APAC} {
		if db != nil && !closed[db] {
			db.Close()
			closed[db] = true
		}
	}
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
