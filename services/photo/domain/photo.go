// Package domain は photo の Entity と Value Object を定義する。
package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ドメインエラー。handler が HTTP ステータスとエラーコードへ対応づける。
var (
	ErrInvalid           = errors.New("photo: 値が不正")
	ErrAlreadyPublished  = errors.New("photo: すでに公開済み")
	ErrUploadNotFinished = errors.New("photo: 画像のアップロードが完了していない")
	ErrNotPending        = errors.New("photo: アップロード待ちではない")
)

// PhotoID は写真の識別子。
type PhotoID int64

// Visibility は公開状態。
type Visibility string

const (
	Private Visibility = "private"
	Public  Visibility = "public"
)

// ParseVisibility は文字列から Visibility を作る。空文字は Private になる。
func ParseVisibility(s string) (Visibility, error) {
	switch Visibility(s) {
	case Private, "":
		return Private, nil
	case Public:
		return Public, nil
	default:
		return "", fmt.Errorf("%w: visibility %q", ErrInvalid, s)
	}
}

// Status は画像のアップロード状態。
type Status string

const (
	// PendingUpload は署名URLを発行済みで、オブジェクトの存在をまだ確認していない状態。
	PendingUpload Status = "pending_upload"
	// Ready はオブジェクトの存在を確認済みで、表示してよい状態。
	Ready Status = "ready"
)

// Caption はキャプション。前後の空白は落とす。
type Caption string

const captionMaxLen = 1000

// ParseCaption は文字列から Caption を作る。
func ParseCaption(s string) (Caption, error) {
	s = strings.TrimSpace(s)
	if len([]rune(s)) > captionMaxLen {
		return "", fmt.Errorf("%w: caption が %d 文字を超える", ErrInvalid, captionMaxLen)
	}
	return Caption(s), nil
}

// Photo は写真投稿の Entity。Repository が行から復元し、usecase がメソッドで状態を変え、
// Repository が行へ書き戻す。同一性は ID で判定する。
type Photo struct {
	id           PhotoID
	ownerSubject string
	caption      Caption
	visibility   Visibility
	gearItemID   *int64
	objectKey    string
	contentType  string
	sizeBytes    *int64
	status       Status
	createdAt    time.Time
	// gearLinkStatus は使用機材の紐付け（同期コマンド）の確定状態。空 = 要求なし。
	gearLinkStatus GearLinkStatus
	// gearLinkKey は紐付けコマンドの冪等キー。回収ジョブが gear への照会に使う。
	gearLinkKey string
}

// NewPhoto は投稿を新規に作る。ID は保存時に確定し、状態は PendingUpload から始まる。
// objectKey は画像の置き場所で、アップロード完了前に決める。
func NewPhoto(ownerSubject string, caption Caption, visibility Visibility, gearItemID *int64, objectKey, contentType string) (*Photo, error) {
	if ownerSubject == "" {
		return nil, fmt.Errorf("%w: 投稿者が空", ErrInvalid)
	}
	if objectKey == "" {
		return nil, fmt.Errorf("%w: object_key が空", ErrInvalid)
	}
	if err := validateContentType(contentType); err != nil {
		return nil, err
	}
	if visibility == "" {
		visibility = Private
	}
	if gearItemID != nil && *gearItemID <= 0 {
		return nil, fmt.Errorf("%w: gear_item_id が不正", ErrInvalid)
	}
	return &Photo{
		ownerSubject: ownerSubject, caption: caption, visibility: visibility, gearItemID: gearItemID,
		objectKey: objectKey, contentType: contentType, status: PendingUpload,
	}, nil
}

// GearLinkStatus は使用機材の紐付け（同期コマンド）の確定状態。
// 空は「要求なし」。pending は Atomic で確定済み・gear の結果待ちで、
// gear 停止中はここに留まり回収ジョブが冪等キー照会で確定させる（docs/02-architecture.md、4.4）。
type GearLinkStatus string

// 紐付けの状態。
const (
	GearLinkNone     GearLinkStatus = ""
	GearLinkPending  GearLinkStatus = "pending"
	GearLinked       GearLinkStatus = "linked"
	GearLinkRejected GearLinkStatus = "rejected"
)

// ErrNoGearItem は機材未指定の写真に紐付けを要求したことを表す。
var ErrNoGearItem = fmt.Errorf("%w: 機材が指定されていない", ErrInvalid)

// ErrLinkNotPending は pending でない紐付けを確定しようとしたことを表す。
var ErrLinkNotPending = errors.New("photo: 紐付けは結果待ちではない")

// RequestGearLink は紐付けを要求済み（pending）にする。key は photo が採番する冪等キーで、
// リトライ・回収のたびに**同じ値**を gear へ送る（キーが変わると重複排除が壊れる）。
func (p *Photo) RequestGearLink(key string) error {
	if p.gearItemID == nil {
		return ErrNoGearItem
	}
	if key == "" {
		return fmt.Errorf("%w: 冪等キーが空", ErrInvalid)
	}
	p.gearLinkStatus = GearLinkPending
	p.gearLinkKey = key
	return nil
}

// ConfirmGearLink は gear の受理（linked）を反映する。
func (p *Photo) ConfirmGearLink() error {
	if p.gearLinkStatus != GearLinkPending {
		return ErrLinkNotPending
	}
	p.gearLinkStatus = GearLinked
	return nil
}

// RejectGearLink は gear の拒否（rejected）を反映する。gear_item_id は消さない——
// 「何に紐付けようとして拒否されたか」が消えると、利用者への表示も調査も成り立たない。
func (p *Photo) RejectGearLink() error {
	if p.gearLinkStatus != GearLinkPending {
		return ErrLinkNotPending
	}
	p.gearLinkStatus = GearLinkRejected
	return nil
}

// AllowedContentTypes はアップロードを受け付ける画像の種類。
var AllowedContentTypes = []string{"image/jpeg", "image/png", "image/webp", "image/avif"}

func validateContentType(ct string) error {
	for _, allowed := range AllowedContentTypes {
		if ct == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: content_type %q は受け付けない", ErrInvalid, ct)
}

// Restored は永続化された行の値。Repository 実装が Restore へ渡す。
type Restored struct {
	ID             PhotoID
	OwnerSubject   string
	Caption        Caption
	Visibility     Visibility
	GearItemID     *int64
	ObjectKey      string
	ContentType    string
	SizeBytes      *int64
	Status         Status
	CreatedAt      time.Time
	GearLinkStatus GearLinkStatus
	GearLinkKey    string
}

// Restore は永続化された行から Entity を復元する。Repository 実装のみが呼ぶ。
func Restore(r Restored) *Photo {
	return &Photo{
		id: r.ID, ownerSubject: r.OwnerSubject, caption: r.Caption, visibility: r.Visibility,
		gearItemID: r.GearItemID, objectKey: r.ObjectKey, contentType: r.ContentType,
		sizeBytes: r.SizeBytes, status: r.Status, createdAt: r.CreatedAt,
		gearLinkStatus: r.GearLinkStatus, gearLinkKey: r.GearLinkKey,
	}
}

func (p *Photo) ID() PhotoID            { return p.id }
func (p *Photo) OwnerSubject() string   { return p.ownerSubject }
func (p *Photo) Caption() Caption       { return p.caption }
func (p *Photo) Visibility() Visibility { return p.visibility }
func (p *Photo) GearItemID() *int64     { return p.gearItemID }

// GearLinkStatus / GearLinkKey は紐付けの確定状態と冪等キー。
func (p *Photo) GearLinkStatus() GearLinkStatus { return p.gearLinkStatus }
func (p *Photo) GearLinkKey() string            { return p.gearLinkKey }
func (p *Photo) ObjectKey() string              { return p.objectKey }
func (p *Photo) ContentType() string            { return p.contentType }
func (p *Photo) SizeBytes() *int64              { return p.sizeBytes }
func (p *Photo) Status() Status                 { return p.status }
func (p *Photo) CreatedAt() time.Time           { return p.createdAt }

// AssignID は保存時に確定した ID を与える。Repository 実装のみが呼ぶ。
func (p *Photo) AssignID(id PhotoID) { p.id = id }

// CommitUpload は画像の存在が確認できたことを受けて Ready へ遷移させる。
func (p *Photo) CommitUpload(sizeBytes int64) error {
	if p.status != PendingUpload {
		return ErrNotPending
	}
	if sizeBytes <= 0 {
		return fmt.Errorf("%w: アップロードされた画像が空", ErrInvalid)
	}
	p.sizeBytes, p.status = &sizeBytes, Ready
	return nil
}

// Publish は公開へ遷移させる。アップロード未完了の写真は公開できない。
func (p *Photo) Publish() error {
	if p.status != Ready {
		return ErrUploadNotFinished
	}
	if p.visibility == Public {
		return ErrAlreadyPublished
	}
	p.visibility = Public
	return nil
}
