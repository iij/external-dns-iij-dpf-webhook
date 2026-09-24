// SPDX-License-Identifier: Apache-2.0

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
	"strconv"
	"strings"

	dpfapi "github.com/iij/dpf-go"
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

	// detail は DPF が返した応答本文。**全文を保持する。**
	//
	// dpf-go の GenericOpenAPIError は Error() に状態コードの文字列しか
	// 載せないため、これがないと 400 が返った理由が分からない。
	// また DPF のエラー応答は `request_id` を含み、サポートへの問い合わせに
	// 必要になる。切り詰めるとその手段が失われる。
	detail string
}

func (e *apiError) Error() string {
	if e.detail == "" {
		return fmt.Sprintf("dpf api: status %d: %v", e.status, e.err)
	}
	return fmt.Sprintf("dpf api: status %d: %v: %s", e.status, e.err, e.detail)
}

func (e *apiError) Unwrap() error { return e.err }

// apiBody は応答本文を取り出せるエラー。dpf.GenericOpenAPIError が満たす。
type apiBody interface {
	Body() []byte
}

// apiStatusError は dpf-go が返す生の API エラー。dpf.GenericOpenAPIError が満たす。
//
// **具体型ではなく振る舞いで受ける。** dpf.GenericOpenAPIError は内部の項目を
// 公開しておらず、状態コードを持つ値をテストから組み立てられない。振る舞いで
// 受ければ、分類の規則そのものを機械的に確かめられる。
type apiStatusError interface {
	error
	apiBody
}

// dpf-go の API エラーが振る舞いを満たすことを、コンパイル時に固定する。
// **満たさなくなると分類が既定の経路へ落ち、恒久的な失敗が一時的になる。**
// 静かに起きる変化であるため、ここで受け止める。
//
//nolint:errcheck // 代入ではなく、型が振る舞いを満たすことの表明である
var _ apiStatusError = (*dpfapi.GenericOpenAPIError)(nil)

// wrapAPIError は DPF API の応答とエラーを apiError に包む。
//
// resp が nil の場合 (接続自体が成立しなかった場合) は err をそのまま返す。
// 状態コードが取れないため、分類はネットワーク層のエラーとして行われる。
//
// body を明示的に渡せる。dpf-go が本文を保持していない経路 (SyncWait の
// ジョブ失敗など) でも内容を残せるようにするため。省略した場合は err から
// 取り出す。
func wrapAPIError(resp *http.Response, err error, body ...[]byte) error {
	if err == nil {
		return nil
	}
	if resp == nil {
		return err
	}

	var raw []byte
	switch {
	case len(body) > 0 && len(body[0]) > 0:
		raw = body[0]
	default:
		var b apiBody
		if errors.As(err, &b) {
			raw = b.Body()
		}
	}

	// **本文は切り詰めない。** DPF のエラー応答は `request_id` を含み、
	// これがサポートへの問い合わせのキーになる。長さで切ると、問い合わせに
	// 必要な情報が失われる。エラー応答自体は短く、ログを埋めることはない。
	return &apiError{
		status: resp.StatusCode,
		err:    err,
		detail: strings.TrimSpace(string(raw)),
	}
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

	// 保持していたロックを他者に奪われた場合も一時的である。適用は反映まで
	// 進んでいないため (dpf-go が編集の context を打ち切る)、やり直せばよい。
	if errors.Is(err, utils.ErrNotLockHolder) {
		return fmt.Errorf("%w: ゾーンのロックを他の操作に奪われました: %w", provider.ErrTemporary, err)
	}

	// 文脈の打ち切りは、時間切れであれ取り消しであれ再試行の余地がある。
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
	}

	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return classifyStatus(apiErr.status, err)
	}

	// dpf-go の内側で完結する経路 (utils.ZoneApplier など) は、応答に手が
	// 届かないため apiError に包めない。生の API エラーから状態コードを読む。
	var statusErr apiStatusError
	if errors.As(err, &statusErr) {
		if status, ok := genericStatus(statusErr); ok {
			return classifyStatus(status, withAPIBody(err, statusErr))
		}
	}

	// 接続が成立しない、名前を解決できないといったネットワーク層の失敗は一時的。
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
	}

	return fmt.Errorf("%w: %w", provider.ErrTemporary, err)
}

// genericStatus は dpf-go が返した API エラーから状態コードを読む。
//
// dpf.GenericOpenAPIError は状態コードを保持しない。**手がかりは Error() の
// 先頭に HTTP の状態行が入ることだけ**である (生成コードが
// `localVarHTTPResponse.Status` を入れ、詳細があればその後ろに続ける)。
// ここから先頭の数値を読む。
//
// 読めない場合は false を返す。分類は既定の経路へ落ち、一時的になる。
// **読み違えて恒久的に倒すより、分類しない方がよい** (Classify の方針)。
func genericStatus(err error) (int, bool) {
	head, _, _ := strings.Cut(err.Error(), " ")

	status, convErr := strconv.Atoi(head)
	if convErr != nil || status < 100 || status > 599 {
		return 0, false
	}
	return status, true
}

// withAPIBody は DPF が返した応答本文をエラーのメッセージへ足す。
//
// dpf.GenericOpenAPIError の Error() は状態行と概要しか載せない。
// **本文がないと 400 が返った理由が分からず、`request_id` も失われる。**
// 本文は切り詰めない (apiError と同じ理由)。
func withAPIBody(err error, body apiBody) error {
	detail := strings.TrimSpace(string(body.Body()))
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
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
