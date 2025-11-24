package db

import (
	"context"
	"database/sql"
	"log"
	"log/slog"
	"os"
	"strconv"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ConnectionConfig holds database connection pool configuration.
type ConnectionConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultConnectionConfig returns the default connection pool configuration.
func DefaultConnectionConfig() ConnectionConfig {
	return ConnectionConfig{
		MaxOpenConns:    25,               // Maximum number of open connections
		MaxIdleConns:    10,               // Maximum number of idle connections
		ConnMaxLifetime: 1 * time.Hour,    // Maximum lifetime of a connection
		ConnMaxIdleTime: 30 * time.Minute, // Maximum idle time of a connection
	}
}

// Open creates and configures a new database connection pool.
// It reads DATABASE_URL from environment and applies connection pool settings.
func Open() *sql.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL not set")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}

	// Apply connection pool configuration
	cfg := getConnectionConfigFromEnv()
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	slog.Info("database connection pool configured",
		slog.Int("max_open_conns", cfg.MaxOpenConns),
		slog.Int("max_idle_conns", cfg.MaxIdleConns),
		slog.Duration("conn_max_lifetime", cfg.ConnMaxLifetime),
		slog.Duration("conn_max_idle_time", cfg.ConnMaxIdleTime))

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}

	slog.Info("database connection established successfully")
	return db
}

// getConnectionConfigFromEnv reads connection pool configuration from environment variables.
// Falls back to default values if not set.
func getConnectionConfigFromEnv() ConnectionConfig {
	cfg := DefaultConnectionConfig()

	if maxOpen := os.Getenv("DB_MAX_OPEN_CONNS"); maxOpen != "" {
		if val, err := strconv.Atoi(maxOpen); err == nil && val > 0 {
			cfg.MaxOpenConns = val
		}
	}

	if maxIdle := os.Getenv("DB_MAX_IDLE_CONNS"); maxIdle != "" {
		if val, err := strconv.Atoi(maxIdle); err == nil && val > 0 {
			cfg.MaxIdleConns = val
		}
	}

	if lifetime := os.Getenv("DB_CONN_MAX_LIFETIME"); lifetime != "" {
		if val, err := time.ParseDuration(lifetime); err == nil && val > 0 {
			cfg.ConnMaxLifetime = val
		}
	}

	if idleTime := os.Getenv("DB_CONN_MAX_IDLE_TIME"); idleTime != "" {
		if val, err := time.ParseDuration(idleTime); err == nil && val > 0 {
			cfg.ConnMaxIdleTime = val
		}
	}

	return cfg
}
