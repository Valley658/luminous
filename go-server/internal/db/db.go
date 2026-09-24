package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

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

// OpenLumi는 루미(AI 마스코트) 대화 기록만 담는 완전히 별도의 DB에 연결한다 -
// 사이트 본 DB(pastellive_db 등)와 테이블로 섞이지 않고 자기만의 DB를 갖는다.
// MySQL 백엔드에서는 같은 MySQL 서버/계정을 그대로 재사용하되 DB 이름만
// cfg.LumiDBName으로 다르며, 처음 실행 시 그 DB가 없으면 자동으로 만든다
// (본 DB는 리도님이 이미 직접 만들어둔 걸 전제로 하지만, 루미 DB는 새로
// 추가되는 것이므로 수동 생성 없이 바로 동작하게 하기 위함). SQLite
// 백엔드에서는 별도 파일(cfg.LumiSQLitePath)을 쓴다.
func OpenLumi(cfg *config.Config) (*DB, error) {
	if cfg.DBBackend == "mysql" {
		if err := ensureMySQLDatabaseExists(cfg.MySQLUser, cfg.MySQLPass, cfg.MySQLHost, cfg.MySQLPort, cfg.LumiDBName); err != nil {
			return nil, fmt.Errorf("루미 DB(%s) 생성 확인 실패: %w", cfg.LumiDBName, err)
		}
		return openMySQL(cfg.MySQLUser, cfg.MySQLPass, cfg.MySQLHost, cfg.MySQLPort, cfg.LumiDBName)
	}
	return openSQLite(cfg.LumiSQLitePath)
}

// mysqlDBNameRe: DB 이름은 항상 우리 설정(.env)에서만 오지 사용자 입력이
// 아니지만, CREATE DATABASE 구문은 자리표시자(?) 파라미터 바인딩을 지원하지
// 않아 문자열을 직접 이어붙여야 하므로 방어적으로 한 번 더 검증한다.
var mysqlDBNameRe = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

func ensureMySQLDatabaseExists(user, pass, host string, port int, dbName string) error {
	if !mysqlDBNameRe.MatchString(dbName) {
		return fmt.Errorf("잘못된 DB 이름: %q", dbName)
	}
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&charset=utf8mb4", user, pass, host, port)
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	_, err = sqlDB.Exec("CREATE DATABASE IF NOT EXISTS `" + dbName + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
	return err
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
