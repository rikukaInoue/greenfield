// Package migrations は gear のスキーママイグレーション（expand / contract 二系統）を
// バイナリに同梱し、`gear migrate` サブコマンドから適用する。
// 履歴テーブルは系統ごとに分離する（gear_migrations_expand / gear_migrations_contract）。
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
	Expand   Series = "expand"   // 追加系。デプロイ前ステップで適用
	Contract Series = "contract" // 削除・変更系。キュー消化として明示実行
)

const (
	// lockWaitTimeout はメタデータロック / 行ロックの待ち上限（秒）。
	// ロック待ちの行列に後続クエリが詰まる事故を防ぐため、適用側が短時間で自ら退く。
	lockWaitTimeout = 5
	maxAttempts     = 3
	retryInterval   = 3 * time.Second
)

// Up は系統の未適用マイグレーションを全て適用する。ロック待ちで失敗した場合はリトライする。
func Up(ctx context.Context, dsn string, s Series) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		m, closeFn, err := open(dsn, s)
		if err != nil {
			return err
		}
		err = m.Up()
		closeFn()
		// 系統にファイルが1つもない（contract キューが空等）場合、golang-migrate はソース走査で ErrNotExist を返す。適用対象なしとして扱う。
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

// Status は系統の現在バージョンと dirty フラグを返す。未適用なら version=0。
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

// openDB はDSNにロック待ちタイムアウトのセッション変数を付与して接続する。
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
		MigrationsTable: "gear_migrations_" + string(s),
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

// isLockTimeout は MySQL のロック待ちタイムアウト（1205: innodb_lock_wait_timeout / 1206 / 3572: lock_wait_timeout 系）か判定する。
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
