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
