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
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/golang-migrate/migrate/v4"
	migratemysql "github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source"
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

// historyTablePrefix は golang-migrate の履歴テーブル名の接頭辞(系統ごとに分ける)。
const historyTablePrefix = "photo_migrations_"

const (
	// lockWaitTimeout はロック待ちの上限（秒）。後続クエリが詰まる前に適用側が退く。
	lockWaitTimeout = 5
	maxAttempts     = 3
	retryInterval   = 3 * time.Second
)

// Up は未適用のマイグレーションを全て適用する。ロック待ちで失敗した場合はリトライする。
// 直前の失敗で dirty が残っていれば解消してから進む。
func Up(ctx context.Context, dsn string, s Series) error {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		m, src, closeFn, err := open(dsn, s)
		if err != nil {
			return err
		}
		if err := resetDirty(m, src); err != nil {
			closeFn()
			return fmt.Errorf("migrate %s: dirty の解消: %w", s, err)
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
	m, _, closeFn, err := open(dsn, s)
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

func open(dsn string, s Series) (*migrate.Migrate, source.Driver, func(), error) {
	db, cfg, err := openDB(dsn)
	if err != nil {
		return nil, nil, nil, err
	}
	driver, err := migratemysql.WithInstance(db, &migratemysql.Config{
		DatabaseName:    cfg.DBName,
		MigrationsTable: historyTablePrefix + string(s),
	})
	if err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("migrate: driver: %w", err)
	}
	sub, err := fs.Sub(files, string(s))
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	src, err := iofs.New(sub, ".")
	if err != nil {
		db.Close()
		return nil, nil, nil, fmt.Errorf("migrate: source: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, cfg.DBName, driver)
	if err != nil {
		db.Close()
		return nil, nil, nil, err
	}
	return m, src, func() { m.Close() }, nil
}

// resetDirty は直前の失敗で残った dirty フラグを、ひとつ前のバージョンへ戻して解消する。
// 1ファイル1文で書く前提のもとでは、失敗したマイグレーションは適用されていないため安全。
// 複数文を1ファイルに書くと部分適用が起こりうるので、その場合は手で確認する。
func resetDirty(m *migrate.Migrate, src source.Driver) error {
	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return nil
	}
	if err != nil {
		return err
	}
	if !dirty {
		return nil
	}
	slog.Warn("直前の失敗で dirty が残っているので戻す", "version", v)

	// 失敗したのが最初のマイグレーションなら、戻し先は「何も適用していない」。
	// **source の先頭と一致するかで判定する**。Prev のエラーで代用してはいけない:
	// 失敗したファイルを消す / 番号を変える（= 前方修正の普通の形）と Prev は
	// 「見つからない」で返り、それを「最初だった」と読むと Force(-1) で
	// **履歴を全消し**してしまう。次の実行は CREATE TABLE から始めて
	// "already exists" で失敗し、以後どのデプロイも通らなくなる（check #24 で観測）。
	if first, ferr := src.First(); ferr == nil && v == first {
		return m.Force(-1)
	}

	prev, err := src.Prev(v)
	if err != nil {
		// v が source に無い。dirty を残したまま止める —— 推測して履歴を書き換えるより、
		// 人が「どこまで適用されたか」を見て force するほうが安全。
		return fmt.Errorf("dirty なバージョン %d が source に無い（失敗したマイグレーションを"+
			"消した/番号を変えた？）。適用状況を確認して `migrate force <version>` で"+
			"解消すること。履歴を推測で書き換えない: %w", v, err)
	}
	return m.Force(int(prev))
}

// isLockTimeout は MySQL のロック待ちタイムアウトか判定する。
// golang-migrate はドライバのエラーを独自の型で包み Unwrap を通さないため、
// errors.As で取れない場合はメッセージも見る。
func isLockTimeout(err error) bool {
	var me *mysql.MySQLError
	if errors.As(err, &me) {
		switch me.Number {
		case 1205, 1206, 3572:
			return true
		}
		return false
	}
	msg := err.Error()
	for _, code := range []string{"Error 1205", "Error 1206", "Error 3572"} {
		if strings.Contains(msg, code) {
			return true
		}
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
