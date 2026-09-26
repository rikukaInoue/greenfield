// Package readmodel は自ドメインの正から組み立てた、読むためだけの形（Read Model）。
// Entityを経由せず、sqlcの参照用クエリからResponse DTOを直接組み立てる。tx外で実行する。
package readmodel
