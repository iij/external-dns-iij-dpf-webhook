// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	dpfapi "github.com/iij/dpf-go"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// contracts/dpf-client.md: 境界を越えるエラーは、一時的か恒久的かに分類済みでなければ
// ならない。この分類が webhook 契約の 4xx / 5xx を決める。
//
// 一時的な障害を 4xx として返すと、ExternalDNS が再試行を諦め、復旧可能な障害が
// 復旧しなくなる。
func TestClassify_Temporary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   error
	}{
		{"応答不能", &net502{}},
		{"HTTP 500", httpStatusError(http.StatusInternalServerError)},
		{"HTTP 502", httpStatusError(http.StatusBadGateway)},
		{"HTTP 503", httpStatusError(http.StatusServiceUnavailable)},
		{"HTTP 504", httpStatusError(http.StatusGatewayTimeout)},
		{"レート制限", httpStatusError(http.StatusTooManyRequests)},
		{"ロック取得不能", utils.ErrStillLock},
		{"ロックの喪失", utils.ErrNotLockHolder},
		{"文脈の期限切れ", context.DeadlineExceeded},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(c.in)
			if !errors.Is(got, provider.ErrTemporary) {
				t.Errorf("Classify(%v) が一時的に分類されていない: %v", c.in, got)
			}
			if errors.Is(got, provider.ErrPermanent) {
				t.Errorf("Classify(%v) が恒久的にも分類されている", c.in)
			}
		})
	}
}

func TestClassify_Permanent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   error
	}{
		{"形式違反", httpStatusError(http.StatusBadRequest)},
		{"認証エラー", httpStatusError(http.StatusUnauthorized)},
		{"権限不足", httpStatusError(http.StatusForbidden)},
		{"対象なし", httpStatusError(http.StatusNotFound)},
		{"ゾーン解決不能", utils.ErrZoneNotFound},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := Classify(c.in)
			if !errors.Is(got, provider.ErrPermanent) {
				t.Errorf("Classify(%v) が恒久的に分類されていない: %v", c.in, got)
			}
			if errors.Is(got, provider.ErrTemporary) {
				t.Errorf("Classify(%v) が一時的にも分類されている", c.in)
			}
		})
	}
}

// FR-038: トークンの取得失敗は恒久的な失敗として扱う。
// 取得できない状態は同じ要求を繰り返しても解消しないため。
// dpf-go 側も TokenError をリトライしない設計であり、この分類と一致する。
func TestClassify_TokenErrorIsPermanent(t *testing.T) {
	t.Parallel()

	cases := []error{
		&utils.TokenError{Err: utils.ErrTokenRequired},
		&utils.TokenError{Err: os.ErrNotExist},
	}

	for _, in := range cases {
		got := Classify(in)
		if !errors.Is(got, provider.ErrPermanent) {
			t.Errorf("Classify(%v) が恒久的に分類されていない: %v", in, got)
		}
	}
}

// FR-039: トークンの値、およびトークンを含むファイルの内容をエラーメッセージに含めない。
func TestClassify_DoesNotLeakTokenValue(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-token-value"

	// トークン取得処理が誤って値を含むエラーを返した場合でも、境界で落とす。
	leaky := &utils.TokenError{Err: errors.New("read token file: content was " + secret)}

	got := Classify(leaky)
	if strings.Contains(got.Error(), secret) {
		t.Errorf("エラーメッセージにトークンが含まれている: %v", got)
	}
}

// DPF が返したエラーの内容がメッセージに含まれる。
//
// 状態コードだけでは何が悪かったのか分からない。実環境で 400 が返ったとき、
// error_type と error_message がなければ原因の切り分けができない。
func TestClassify_IncludesAPIErrorDetail(t *testing.T) {
	t.Parallel()

	body := `{"request_id":"abc123","error_type":"ParameterError","error_message":"records は必須です"}`
	err := wrapAPIError(
		&http.Response{StatusCode: http.StatusBadRequest},
		&dpfapi.GenericOpenAPIError{},
		[]byte(body),
	)

	got := Classify(err).Error()
	for _, want := range []string{"ParameterError", "records は必須です", "abc123"} {
		if !strings.Contains(got, want) {
			t.Errorf("エラーメッセージに %q が含まれない: %v", want, got)
		}
	}
}

// 応答本文を切り詰めない。
//
// DPF のエラー応答は request_id を含み、サポートへの問い合わせのキーになる。
// 長さで切ると、末尾にある情報が失われて問い合わせができなくなる。
func TestClassify_DoesNotTruncateAPIErrorBody(t *testing.T) {
	t.Parallel()

	// request_id が末尾にある長い応答を模す。
	long := `{"error_type":"ParameterError","error_message":"` +
		strings.Repeat("詳細な説明", 200) + `","request_id":"tail-request-id"}`

	err := wrapAPIError(
		&http.Response{StatusCode: http.StatusBadRequest},
		&dpfapi.GenericOpenAPIError{},
		[]byte(long),
	)

	got := Classify(err).Error()
	if !strings.Contains(got, "tail-request-id") {
		t.Errorf("末尾の request_id が失われた。問い合わせができなくなる (長さ %d)", len(got))
	}
	if strings.Contains(got, "以下略") {
		t.Error("応答本文が切り詰められている")
	}
}

// 応答本文がない場合も、状態コードは失われない。
func TestClassify_NoBodyStillReportsStatus(t *testing.T) {
	t.Parallel()

	err := wrapAPIError(&http.Response{StatusCode: http.StatusForbidden}, &dpfapi.GenericOpenAPIError{}, nil)

	if got := Classify(err).Error(); !strings.Contains(got, "403") {
		t.Errorf("状態コードが失われた: %v", got)
	}
}

// 分類できないエラーは一時的に倒す。
//
// 恒久的に倒すと ExternalDNS が再試行を諦め、実際には復旧しうる障害で
// DNS が更新されなくなる。判断がつかない場合は再試行の余地を残す。
func TestClassify_UnknownIsTemporary(t *testing.T) {
	t.Parallel()

	got := Classify(errors.New("何か未知の失敗"))
	if !errors.Is(got, provider.ErrTemporary) {
		t.Errorf("未知のエラーが一時的に分類されていない: %v", got)
	}
}

// nil はそのまま nil で返る。
func TestClassify_Nil(t *testing.T) {
	t.Parallel()

	if got := Classify(nil); got != nil {
		t.Errorf("Classify(nil) = %v, want nil", got)
	}
}

// 分類済みのエラーを再分類しても、分類は変わらない。
func TestClassify_Idempotent(t *testing.T) {
	t.Parallel()

	once := Classify(httpStatusError(http.StatusForbidden))
	twice := Classify(once)

	if !errors.Is(twice, provider.ErrPermanent) {
		t.Errorf("再分類で恒久的でなくなった: %v", twice)
	}
	if errors.Is(twice, provider.ErrTemporary) {
		t.Errorf("再分類で一時的にもなった: %v", twice)
	}
}

// dpf-go の内側で完結する経路のエラーも、状態コードで分類される。
//
// [utils.ZoneApplier] を使う適用の経路では、応答 (*http.Response) に手が
// 届かないため apiError に包めない。**ここが効かないと、DPF が 400 で拒む
// 要求を ExternalDNS が永久に再試行する。**
func TestClassify_RawAPIErrorUsesStatusLine(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		status    string
		permanent bool
	}{
		{"形式違反", "400 Bad Request", true},
		{"権限不足", "403 Forbidden", true},
		{"レート制限", "429 Too Many Requests", false},
		{"DPF の障害", "503 Service Unavailable", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := Classify(&rawAPIError{message: c.status})

			if c.permanent && !errors.Is(got, provider.ErrPermanent) {
				t.Errorf("Classify(%q) が恒久的に分類されていない: %v", c.status, got)
			}
			if !c.permanent && !errors.Is(got, provider.ErrTemporary) {
				t.Errorf("Classify(%q) が一時的に分類されていない: %v", c.status, got)
			}
		})
	}
}

// 生の API エラーでも、DPF が返した応答本文は失われない。
//
// 状態行だけでは 400 の理由が分からず、`request_id` も残らない。
func TestClassify_RawAPIErrorKeepsBody(t *testing.T) {
	t.Parallel()

	body := `{"request_id":"abc123","error_type":"ParameterError","error_message":"records は必須です"}`

	got := Classify(&rawAPIError{message: "400 Bad Request", body: []byte(body)}).Error()
	for _, want := range []string{"ParameterError", "records は必須です", "abc123"} {
		if !strings.Contains(got, want) {
			t.Errorf("エラーメッセージに %q が含まれない: %v", want, got)
		}
	}
}

// 状態行を読み取れない場合は一時的に倒す。
//
// 読み違えて恒久的に倒すと、復旧しうる障害で DNS の更新が止まる。
func TestClassify_RawAPIErrorWithoutStatusIsTemporary(t *testing.T) {
	t.Parallel()

	for _, message := range []string{"", "unexpected EOF", "999 Nonexistent"} {
		got := Classify(&rawAPIError{message: message})
		if !errors.Is(got, provider.ErrTemporary) {
			t.Errorf("Classify(%q) が一時的に分類されていない: %v", message, got)
		}
		if errors.Is(got, provider.ErrPermanent) {
			t.Errorf("Classify(%q) が恒久的にも分類されている: %v", message, got)
		}
	}
}

// rawAPIError は dpf-go の GenericOpenAPIError を模す。
//
// 本物は状態コードも本文も公開しておらず、値を持つものをテストから
// 組み立てられない。分類が見ているもの (状態行を先頭に持つメッセージと本文) を
// そのまま備える。
type rawAPIError struct {
	message string
	body    []byte
}

func (e *rawAPIError) Error() string { return e.message }
func (e *rawAPIError) Body() []byte  { return e.body }

// httpStatusError は指定の状態コードを伴う dpf-go 由来のエラーを模す。
func httpStatusError(code int) error {
	return &apiError{
		status: code,
		err:    &dpfapi.GenericOpenAPIError{},
	}
}

// net502 は接続自体が成立しない状況を模す。
type net502 struct{}

func (*net502) Error() string { return "dial tcp: connection refused" }
func (*net502) Timeout() bool { return false }
func (*net502) Temporary() bool {
	return true
}
