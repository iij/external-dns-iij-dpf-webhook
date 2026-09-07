package provider

import (
	"context"
	"errors"
)

// 本ファイルは、provider が DPF クライアント層に期待する振る舞いを宣言する。
//
// インタフェースを利用側で宣言するのは、境界の形を上位層の必要に合わせるためである。
// 実装は internal/dpf が提供する。原則 II が求めるとおり、ここに dpf-go の生成型や
// ライブラリ固有のエラー型は現れない。
//
// テストではこのインタフェースを差し替える。上位層のテストが実際の DPF API に
// 到達しないことは、原則 II の MUST である。

// 境界を越えるエラーの分類。
//
// この 2 つが webhook 契約の 4xx / 5xx を決める。DPF クライアント層は、
// 境界を越える前にいずれかへ分類する責任を負う。
var (
	// ErrTemporary は一時的な障害を表す。ExternalDNS は再試行してよい。
	// DPF の応答不能、レート制限、ロック取得不能、反映の時間切れが該当する。
	ErrTemporary = errors.New("provider: temporary failure")

	// ErrPermanent は恒久的な障害を表す。再試行しても解消しない。
	// 形式違反、権限不足、ゾーン解決不能、トークン取得失敗が該当する。
	ErrPermanent = errors.New("provider: permanent failure")

	// ErrUnsupportedType は対応リスト外のレコード種別を表す。恒久的な障害の一種。
	ErrUnsupportedType = errors.New("provider: unsupported record type")

	// ErrZoneNotFound は書き込み先のゾーンを解決できないことを表す。恒久的な障害の一種。
	ErrZoneNotFound = errors.New("provider: zone not found")
)

// Backend は DPF クライアント層が提供する操作の全体。
type Backend interface {
	ZoneLister
	RecordLister
	ChangeApplier
}

// ZoneLister は操作可能なゾーンを列挙する。
type ZoneLister interface {
	// ListZones は本 provider が操作できるゾーンを返す。
	// ゾーン名は正規化名として返る。
	ListZones(ctx context.Context) ([]Zone, error)
}

// RecordLister は反映済みのレコードを取得する。
type RecordLister interface {
	// ListRecords は zone の反映済みレコードを全件返す。
	//
	// ページングは境界の内側で完結する。許可リスト外の種別は除外され、
	// 名前は正規化名として返る。
	ListRecords(ctx context.Context, zone Zone) ([]Record, error)
}

// ChangeApplier は変更セットをゾーンへ適用する。
type ChangeApplier interface {
	// Apply は zone に対して cs を適用し、反映が完了するまで待つ。
	//
	// 呼び出し側は「変更セットを渡す」だけでよい。ゾーンの現在の内容を読み直して
	// マージし、管理対象外のレコードを保全する処理は境界の内側で行われる。
	// この実装事情を上位層は知らない。
	//
	// 反映完了前に成功を返さない (FR-011)。途中で中止した場合も成功を返さない
	// (FR-012)。エラーは [ErrTemporary] または [ErrPermanent] に分類済みで返る。
	Apply(ctx context.Context, zone Zone, cs ChangeSet) error
}
