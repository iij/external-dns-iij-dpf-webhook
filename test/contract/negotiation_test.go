// Package contract は ExternalDNS webhook provider API の契約テストを収める。
//
// 原則 I により、契約テストが仕様の唯一の実行可能な表現である。仕様の解釈が
// 必要な箇所は実装コードではなくここに落とす。上流仕様が変わった場合は、
// まずここを更新し、その差分をレビュー対象とする。
//
// 正典は kubernetes-sigs/external-dns v0.22.0 の api/webhook.yaml である。
package contract

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/webhook"
)

// 上流仕様が定めるメディアタイプ。version パラメータを含む。
const mediaType = "application/external.dns.webhook+json;version=1"

// 応答の Content-Type にメディアタイプを設定する。
func TestNegotiate_SetsContentType(t *testing.T) {
	t.Parallel()

	h := webhook.Negotiate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", mediaType)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if got := rec.Code; got != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", got, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != mediaType {
		t.Errorf("Content-Type = %q, want %q", got, mediaType)
	}
}

// Accept ヘッダの表記ゆれを受け入れる。
//
// 空白や引用符の有無、パラメータの順序は送信側の実装に依存する。
// これらを理由に拒否すると、上流の実装変更で動かなくなる。
func TestNegotiate_AcceptsEquivalentFormats(t *testing.T) {
	t.Parallel()

	cases := []string{
		"application/external.dns.webhook+json;version=1",
		"application/external.dns.webhook+json; version=1",
		"application/external.dns.webhook+json;version=1;q=1.0",
		"application/external.dns.webhook+json;Version=1",
		"*/*",
		"", // Accept 未指定は「何でもよい」と解釈する
	}

	for _, accept := range cases {
		h := webhook.Negotiate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Accept=%q で状態コード = %d, want %d", accept, rec.Code, http.StatusOK)
		}
	}
}

// ネゴシエートできない要求は受け付けない。
func TestNegotiate_RejectsUnsupportedMediaType(t *testing.T) {
	t.Parallel()

	cases := []string{
		"application/json",
		"text/plain",
		"application/external.dns.webhook+json;version=2",
	}

	for _, accept := range cases {
		h := webhook.Negotiate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			t.Errorf("Accept=%q が受け入れられた。ネゴシエートできない要求は拒否する", accept)
		}
	}
}

// エラーの分類と状態コードの対応。
//
// contracts/webhook-api.md:
//
//	一時的な障害 → 5xx (ExternalDNS は再試行する)
//	恒久的な障害 → 4xx (ExternalDNS は再試行しない)
//
// 一時的な障害を 4xx として返すと、ExternalDNS の再試行判断を誤らせ、
// 復旧可能な障害が復旧しなくなる。
func TestWriteError_StatusMapping(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"一時的", provider.ErrTemporary, http.StatusInternalServerError},
		{"一時的 (包まれている)", errors.Join(provider.ErrTemporary, errors.New("詳細")), http.StatusInternalServerError},
		{"恒久的", provider.ErrPermanent, http.StatusBadRequest},
		{"未対応種別", provider.ErrUnsupportedType, http.StatusBadRequest},
		{"ゾーン解決不能", provider.ErrZoneNotFound, http.StatusBadRequest},
		{"分類なし", errors.New("未分類"), http.StatusInternalServerError},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			webhook.WriteError(rec, c.err)

			if rec.Code != c.wantCode {
				t.Errorf("状態コード = %d, want %d", rec.Code, c.wantCode)
			}
		})
	}
}

// 一時的な障害が 4xx になっていないことを、分類側から確かめる。
func TestWriteError_TemporaryIsNever4xx(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	webhook.WriteError(rec, errors.Join(provider.ErrTemporary, errors.New("DPF が応答しません")))

	if rec.Code >= 400 && rec.Code < 500 {
		t.Errorf("一時的な障害が %d で返された。4xx にしてはならない", rec.Code)
	}
}

// エラー応答に内部の詳細を載せない。
//
// exposed エンドポイントではないが、DNS 構成やトークンが読み取れる情報を
// 応答本文へ出す理由はない。
func TestWriteError_DoesNotEchoInternalDetail(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-token-value"

	rec := httptest.NewRecorder()
	webhook.WriteError(rec, errors.Join(provider.ErrPermanent, errors.New("token was "+secret)))

	if body := rec.Body.String(); contains(body, secret) {
		t.Errorf("応答本文に内部の詳細が含まれている: %q", body)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
