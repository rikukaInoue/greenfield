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
	"strings"
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

// historyTablePrefix は golang-migrate の履歴テーブル名の接頭辞(系統ごとに分ける)。
const historyTablePrefix = "gear_migrations_"

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
		MigrationsTable: historyTablePrefix + string(s),
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

// PendingCount は系統 s の未適用マイグレーション数を返す(#200)。
// contract は「フラグ100%到達 → 旧経路削除 → 実行」のキュー消化なので、
// 忘れると滞留したまま誰も気づかない。常時メトリクスにするための読み取り専用ヘルパ。
// 履歴テーブルが無い(=一度も適用していない)は「全件未適用」として数える。
func PendingCount(ctx context.Context, db *sql.DB, s Series) (int, error) {
	versions, err := embeddedVersions(s)
	if err != nil {
		return 0, err
	}
	applied, err := appliedVersion(ctx, db, s)
	if err != nil {
		return 0, err
	}
	return pendingCount(versions, applied), nil
}

func pendingCount(versions []uint64, applied uint64) int {
	n := 0
	for _, v := range versions {
		if v > applied {
			n++
		}
	}
	return n
}

// embeddedVersions は埋め込みファイル名(NNNNNN_name.up.sql)からバージョン一覧を読む。
func embeddedVersions(s Series) ([]uint64, error) {
	entries, err := fs.ReadDir(files, string(s))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // 系統にファイルが1つもない(gear の contract 等)
		}
		return nil, err
	}
	var out []uint64
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		i := strings.IndexByte(name, '_')
		if i <= 0 {
			return nil, fmt.Errorf("migrations: バージョンが読めないファイル名: %s", name)
		}
		var v uint64
		if _, err := fmt.Sscanf(name[:i], "%d", &v); err != nil {
			return nil, fmt.Errorf("migrations: バージョンが読めないファイル名: %s", name)
		}
		out = append(out, v)
	}
	return out, nil
}

// appliedVersion は履歴テーブルの適用済みバージョンを読む。テーブルが無ければ 0。
// dirty(直前の失敗)は Up が解消するのでここでは区別しない。
func appliedVersion(ctx context.Context, db *sql.DB, s Series) (uint64, error) {
	var v uint64
	err := db.QueryRowContext(ctx,
		"SELECT version FROM "+historyTablePrefix+string(s)+" LIMIT 1").Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == 1146 { // テーブルが無い = 未適用
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return v, nil
}
