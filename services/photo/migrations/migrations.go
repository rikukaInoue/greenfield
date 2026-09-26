// Package migrations は photo のスキーママイグレーションを埋め込み、migrate サブコマンドから適用する。
// 系統（expand / contract）ごとに履歴テーブルを分ける。
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed all:expand all:contract
var files embed.FS

// Series はマイグレーションの系統。
type Series string

const (
	Expand   Series = "expand"   // 追加系。デプロイ前に適用
	Contract Series = "contract" // 削除・変更系。キュー消化として明示実行
)

const (
	// lockWaitTimeout はロック待ちの上限（秒）。後続クエリが詰まる前に適用側が退く。
	lockWaitTimeout = 5
	maxAttempts     = 3
	retryInterval   = 3 * time.Second
)

// Up は未適用のマイグレーションを全て適用する。ロック待ちで失敗した場合はリトライする。
func Up(ctx context.Context, dsn string, s Series) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		m, closeFn, err := open(dsn, s)
		if err != nil {
			return err
		}
		err = m.Up()
		closeFn()
		// ファイルが1つもない系統では golang-migrate が ErrNotExist を返す。適用対象なしとして扱う
		if err == nil || errors.Is(err, migrate.ErrNoChange) || errors.Is(err, fs.ErrNotExist) {
			if err != nil {
				slog.Info("migrate: no change", "series", s)
			}
			return nil
		}
		lastErr = err
		if !isLockTimeout(err) || attempt == maxAttempts {
			break
		}
		slog.Warn("migrate: lock wait timeout, retrying", "series", s, "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
		}
	}
	return fmt.Errorf("migrate %s up: %w", s, lastErr)
}

// Status は現在バージョンと dirty フラグを返す。未適用なら 0。
func Status(dsn string, s Series) (version uint, dirty bool, err error) {
	m, closeFn, err := open(dsn, s)
	if err != nil {
		return 0, false, err
	}
	defer closeFn()
	version, dirty, err = m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, nil
	}
	return version, dirty, err
}

// openDB はロック待ちタイムアウトを付与して接続する。
func openDB(dsn string) (*sql.DB, *mysql.Config, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("migrate: parse dsn: %w", err)
	}
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["lock_wait_timeout"] = fmt.Sprint(lockWaitTimeout)
	cfg.Params["innodb_lock_wait_timeout"] = fmt.Sprint(lockWaitTimeout)
	cfg.MultiStatements = true

	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, nil, err
	}
	return db, cfg, nil
}

func open(dsn string, s Series) (*migrate.Migrate, func(), error) {
	db, cfg, err := openDB(dsn)
	if err != nil {
		return nil, nil, err
	}
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{
		DatabaseName:    cfg.DBName,
		MigrationsTable: "photo_migrations_" + string(s),
	})
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate: driver: %w", err)
	}
	sub, err := fs.Sub(files, string(s))
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("migrate: source: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, cfg.DBName, driver)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return m, func() { m.Close() }, nil
}

// isLockTimeout は MySQL のロック待ちタイムアウトか判定する。
func isLockTimeout(err error) bool {
	var me *mysql.MySQLError
	if !errors.As(err, &me) {
		return false
	}
	switch me.Number {
	case 1205, 1206, 3572:
		return true
	}
	return false
}
