package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"

	"pastellive/internal/config"
)

type DB struct {
	*sql.DB
	Backend string
}

func Open(cfg *config.Config) (*DB, error) {
	if cfg.DBBackend == "mysql" {
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&loc=Local",
			cfg.MySQLUser, cfg.MySQLPass, cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLName)
		sqlDB, err := sql.Open("mysql", dsn)
		if err != nil {
			return nil, err
		}
		if err := sqlDB.Ping(); err != nil {
			return nil, err
		}
		return &DB{DB: sqlDB, Backend: "mysql"}, nil
	}

	if err := os.MkdirAll(filepath.Dir(cfg.SQLitePath), 0o755); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=foreign_keys(ON)", cfg.SQLitePath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return &DB{DB: sqlDB, Backend: "sqlite"}, nil
}

func (d *DB) NowExpr() string {
	if d.Backend == "mysql" {
		return "NOW()"
	}
	return "datetime('now','localtime')"
}
