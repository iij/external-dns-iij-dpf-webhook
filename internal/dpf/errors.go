// Package dpf は DPF API へのアクセスを担う。
//
// 原則 II により、github.com/iij/dpf-go への依存は本パッケージの内側に閉じる。
// 生成された API 型 (dpf.Record など)、ライブラリ固有のエラー型、HTTP の詳細、
// および認証情報は、このパッケージの外へ出さない。上位層とは
// internal/provider/ports.go のインタフェースで結合する。
package dpf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// apiError は DPF API が返した状態コードを伴うエラー。
//
// dpf-go の GenericOpenAPIError は状態コードを保持しないため、呼び出し箇所で
// *http.Response から拾って包む。
type apiError struct {
	status int
	err    error
}

func (e *apiError) Error() string {
	return fmt.Sprintf("dpf api: status %d: %v", e.status, e.err)
}

func (e *apiError) Unwrap() error { return e.err }

// wrapAPIError は DPF API の応答とエラーを apiError に包む。
// resp が nil の場合 (接続自体が成立しなかった場合) は err をそのまま返す。
func wrapAPIError(resp *http.Response, err error) error {
	if err == nil {
		return nil
	}
	if resp == nil {
		return err
	}
	return &apiError{status: resp.StatusCode, err: err}
}

// Classify は err を一時的な障害と恒久的な障害のいずれかに分類する。
//
// 分類は webhook 契約の状態コードを決める。一時的なら 5xx を返して ExternalDNS に
// 再試行させ、恒久的なら 4xx を返して再試行させない。
//
// 判断がつかないエラーは一時的に倒す。恒久的に倒すと ExternalDNS が再試行を諦め、
// 実際には復旧しうる障害で DNS の更新が止まる。誤って再試行する方が、
// 誤って諦めるより害が小さい。
//
// 戻り値のメッセージに認証情報を含めない (FR-039)。
func Classify(err error) error {
	if err == nil {
		return nil
	}

	// すでに分類済みならそのまま返す。二重に包まない。
	if errors.Is(err, provider.ErrTemporary) || errors.Is(err, provider.ErrPermanent) {
		return err
	}

	// トークンの取得失敗は恒久的。取得できない状態は同じ要求を繰り返しても解消しない
	// (FR-038)。dpf-go 側も TokenError をリトライしない設計であり、これと一致する。
	//
	// メッセージは定型文に置き換える。トークンの取得処理が値やファイル内容を
	// 含むエラーを返した場合に、それを外へ流さないため (FR-039)。
	var tokenErr *utils.TokenError
	if errors.As(err, &tokenErr) {
		return fmt.Errorf("%w: アクセストークンを取得できませんでした", provider.ErrPermanent)
	}

	if errors.Is(err, utils.ErrZoneNotFound) {
		return fmt.Errorf("%w: %w", provider.ErrPermanent, provider.ErrZoneNotFound)
	}

	// ロックを取得できないのは、他の適用が進行中であることを意味する。時間をおけば解消する。
	if errors.Is(err, utils.ErrStillLock) {
		return fmt.Errorf("%w: ゾーンが他の操作でロックされています: %w", provider.ErrTemporary, err)
	}

	// 文脈の打ち切りは、時間切れであれ取り消しであれ再試行の余地がある。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
	}

	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return classifyStatus(apiErr.status, err)
	}

	// 接続が成立しない、名前を解決できないといったネットワーク層の失敗は一時的。
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
	}

	return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
}

// classifyStatus は HTTP 状態コードから分類を決める。
//
// contracts/webhook-api.md の対応に従う。5xx と 429 が一時的、それ以外の 4xx が恒久的。
func classifyStatus(status int, err error) error {
	switch {
	case status == http.StatusTooManyRequests:
		return fmt.Errorf("%w: レート制限に達しました: %w", provider.ErrTemporary, err)

	case status >= 500:
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)

	case status == http.StatusRequestTimeout:
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)

	case status >= 400:
		return fmt.Errorf("%w: %w", provider.ErrPermanent, err)

	default:
		// 2xx / 3xx でエラーが立つのは想定外。判断がつかないため一時的に倒す。
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
	}
}
