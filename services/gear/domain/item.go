// Package domain は gear の業務ルール。機材（GearItem）はカタログデータであり、
// 公開/非公開の状態を持たない（投稿されたら全員に見える。写真と違い秘匿の要件が無い）。
package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Kinds は機材の分類。増やすときはここに足す（DB は VARCHAR。ENUM の変更は COPY になりうる）。
var Kinds = []string{"camera", "lens", "tripod", "filter", "bag", "accessory"}

// GearItem は機材。
type GearItem struct {
	id        int64
	kind      string
	name      string
	maker     string
	createdBy string
}

// NewGearItem は投稿内容を検証して機材を作る。
func NewGearItem(createdBy, kind, name, maker string) (*GearItem, error) {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if !slices.Contains(Kinds, kind) {
		return nil, fmt.Errorf("kind は %v のいずれか: %q", Kinds, kind)
	}
	if name == "" || len(name) > 120 {
		return nil, errors.New("name は 1〜120 文字")
	}
	if len(maker) > 120 {
		return nil, errors.New("maker は 120 文字まで")
	}
	if createdBy == "" {
		return nil, errors.New("createdBy が空")
	}
	return &GearItem{kind: kind, name: name, maker: strings.TrimSpace(maker), createdBy: createdBy}, nil
}

// Restore は保存済みの行から復元する（repository 専用）。
func Restore(id int64, kind, name, maker, createdBy string) *GearItem {
	return &GearItem{id: id, kind: kind, name: name, maker: maker, createdBy: createdBy}
}

func (g *GearItem) ID() int64         { return g.id }
func (g *GearItem) Kind() string      { return g.kind }
func (g *GearItem) Name() string      { return g.name }
func (g *GearItem) Maker() string     { return g.maker }
func (g *GearItem) CreatedBy() string { return g.createdBy }

// SetID は保存後に確定した ID を書き戻す（repository 専用）。
func (g *GearItem) SetID(id int64) { g.id = id }
