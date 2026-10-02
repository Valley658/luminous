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

// Open은 사이트 본 DB(회원/영상/팬아트 등 대부분의 테이블이 사는 곳,
// MySQL이면 cfg.MySQLName/SQLite면 cfg.SQLitePath)에 연결한다.
func Open(cfg *config.Config) (*DB, error) {
	if cfg.DBBackend == "mysql" {
		return openMySQL(cfg.MySQLUser, cfg.MySQLPass, cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLName)
	}
	return openSQLite(cfg.SQLitePath)
}

func openMySQL(user, pass, host string, port int, dbName string) (*DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&charset=utf8mb4&loc=Local", user, pass, host, port, dbName)
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, err
	}
	return &DB{DB: sqlDB, Backend: "mysql"}, nil
}

func openSQLite(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(30000)&_pragma=foreign_keys(ON)", path)
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
