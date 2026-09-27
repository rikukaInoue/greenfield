## 2026-09-27 — status の既定を fail-closed にし、実体のない `ready` を DB で禁じる（監査 D-4 / #122）

### 作ったもの

- `contract/000002_status_fail_closed`:
  1. `status` の既定を `ready` → `pending_upload`
  2. `status='ready' AND object_key IS NULL` の行を `pending_upload` へ落とす（CHECK の前提）
  3. `CHECK (status <> 'ready' OR object_key IS NOT NULL)`

### 確認（MySQL 8.4.11、実測）

| 確認 | 結果 |
|---|---|
| `ALTER COLUMN status SET DEFAULT`、`ALGORITHM = INSTANT` | **通る**（メタデータのみ。既存行の値は変わらない） |
| `ADD CONSTRAINT ... CHECK`、`ALGORITHM = INSTANT` | **不可** `ERROR 1845` → `Try ALGORITHM=COPY` |
| 同 `ALGORITHM = INPLACE, LOCK = NONE` | **不可** `ERROR 1845`（同じ） |
| `status` を省いた `INSERT` | `pending_upload` で入る（以前は `ready` = 即座に一覧へ出ていた） |
| `INSERT ... status='ready'` で `object_key` なし | `ERROR 3819` Check constraint violated |
| `UPDATE` で `status='ready', object_key=NULL` に壊す | `ERROR 3819`（入口も出口も塞がっている） |
| 正常な commit 相当（`object_key` ありで `ready` へ） | 通る |
| `schema.sql`（マイグレーション全適用 → `mysqldump`）| 更新済み。`--check` が一致、`sqlc diff` も差分なし |

### 気づき

1. **CHECK 追加は COPY しかない。** internal-05 の「INSTANT → INPLACE → どちらも通らなければ例外として個別計画」の
   例外に該当する。greenfield の行数では一瞬だが、本番規模の `photos` では行の再構築になるので、
   gh-ost / pt-online-schema-change か保守時間の計画が要る。マイグレーションに方式（`ALGORITHM = COPY, LOCK = SHARED`）を
   明示したので、黙って長時間ロックする形にはならない
2. **fail-open な既定は「一度だけ正しい」。** `DEFAULT 'ready'` はカラム追加時に既存行を見えるままにするために
   正しかったが、その役目が済んだ後も残り、`status` を忘れた INSERT が回復不能な行を作る入口になっていた。
   役目の終わった既定を落とす契機が contract キューだった
3. **ホストの mysqld と衝突していた。** `schema-dump.sh` はコンテナ経由で `mysql` を叩くが、
   マイグレーションは**ホストから TCP** で繋ぐので、ホストで別の mysqld が 3306 を掴んでいると
   そちらへ当たって `Access denied` になる。今回は `-p 13306:3306` と `MYSQL_PORT=13306` で回避した。
   スクリプトが `MYSQL_PORT` を見る作りになっていたので追加の変更は要らなかった
