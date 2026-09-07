// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/webhook"
)

// filtersResponse は GET / の応答形。
//
// 上流仕様 (api/webhook.yaml v0.22.0) の schema "filters" に対応する。
// 実装側の型ではなくここで宣言するのは、契約テストが実装から独立であるため。
type filtersResponse struct {
	Filters []string `json:"filters"`
}

func newHandler(t *testing.T, scope dnsname.Scope, backend provider.Backend) http.Handler {
	t.Helper()
	return webhook.NewHandler(provider.New(scope, backend, discardLogger(t)))
}

// FR-001: 管理対象ドメインを ExternalDNS に通知できる。
func TestGetRoot_ReturnsFilters(t *testing.T) {
	t.Parallel()

	scope := dnsname.NewScope(
		dnsname.MustParse("example.jp"),
		dnsname.MustParse("example.com"),
	)
	h := newHandler(t, scope, providertest.New())

	rec := doGet(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != mediaType {
		t.Errorf("Content-Type = %q, want %q", got, mediaType)
	}

	var body filtersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("応答が JSON として解釈できない: %v\n%s", err, rec.Body.String())
	}

	want := map[string]bool{"example.jp.": true, "example.com.": true}
	if len(body.Filters) != len(want) {
		t.Fatalf("filters = %v, want %d 件", body.Filters, len(want))
	}
	for _, f := range body.Filters {
		if !want[f] {
			t.Errorf("想定外の filter %q (want %v)", f, want)
		}
	}
}

// FR-002: 管理対象ドメインが未設定なら、空の範囲を返す。
//
// 「全ドメインを管理する」という応答を返してはならない。この誤りが
// default-deny を破る最短経路である。
func TestGetRoot_EmptyScopeReturnsEmptyFilters(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(), providertest.New())

	rec := doGet(t, h, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", rec.Code, http.StatusOK)
	}

	var body filtersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("応答が JSON として解釈できない: %v\n%s", err, rec.Body.String())
	}

	if len(body.Filters) != 0 {
		t.Errorf("filters = %v, want 空。未設定を全ドメイン許可と解釈してはならない", body.Filters)
	}

	// filters キー自体は存在すること。省略すると受け取り側の解釈が分かれる。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("応答が JSON オブジェクトでない: %v", err)
	}
	if _, ok := raw["filters"]; !ok {
		t.Error("filters キーが応答に存在しない")
	}
}

// 応答は JSON オブジェクトであり、配列ではない。
func TestGetRoot_ResponseIsObject(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())
	rec := doGet(t, h, "/")

	var obj map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &obj); err != nil {
		t.Fatalf("応答が JSON オブジェクトとして解釈できない: %v\n%s", err, rec.Body.String())
	}
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", mediaType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
