package integration

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/webhook"
)

const mediaType = "application/external.dns.webhook+json;version=1"

// FR-016: 一時的な障害と恒久的な障害を区別して ExternalDNS に伝える。
//
// DPF が応答しないのは一時的な障害であり、5xx で返して再試行させる。
// 4xx で返すと ExternalDNS が諦め、復旧後も DNS が更新されない。
func TestListRecords_TemporaryFailureIs5xx(t *testing.T) {
	t.Parallel()

	backend := providertest.New()
	backend.ListZonesErr = errors.Join(provider.ErrTemporary, errors.New("DPF が応答しません"))

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)
	rec := doGet(t, h, "/records")

	if rec.Code < 500 {
		t.Errorf("状態コード = %d, want 5xx。一時的な障害を 4xx にしてはならない", rec.Code)
	}
}

// 恒久的な障害は 4xx で返す。再試行しても解消しない。
func TestListRecords_PermanentFailureIs4xx(t *testing.T) {
	t.Parallel()

	backend := providertest.New()
	backend.ListZonesErr = errors.Join(provider.ErrPermanent, errors.New("権限がありません"))

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)
	rec := doGet(t, h, "/records")

	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("状態コード = %d, want 4xx", rec.Code)
	}
}

// レコード取得の途中で失敗した場合も、部分的な結果を成功として返さない。
//
// 一部のゾーンだけが返ると、ExternalDNS は欠けたレコードを「存在しない」と
// 判断し、作り直すか、既存を削除しにいく。
func TestListRecords_PartialFailureIsNotSuccess(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().WithZone(zone,
		rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"))
	backend.ListRecordsErr = errors.Join(provider.ErrTemporary, errors.New("一覧の取得に失敗"))

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)
	r := doGet(t, h, "/records")

	if r.Code == http.StatusOK {
		t.Errorf("状態コード = %d。取得に失敗したのに成功を返している", r.Code)
	}
}

// 分類のないエラーは 5xx で返す。判断がつかないときは再試行の余地を残す。
func TestListRecords_UnclassifiedFailureIs5xx(t *testing.T) {
	t.Parallel()

	backend := providertest.New()
	backend.ListZonesErr = errors.New("未分類の失敗")

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)
	r := doGet(t, h, "/records")

	if r.Code < 500 {
		t.Errorf("状態コード = %d, want 5xx", r.Code)
	}
}

// エラー応答に内部の詳細を載せない。
func TestListRecords_ErrorBodyHasNoDetail(t *testing.T) {
	t.Parallel()

	const secret = "internal-zone-name.example"

	backend := providertest.New()
	backend.ListZonesErr = errors.Join(provider.ErrTemporary, errors.New(secret))

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)
	r := doGet(t, h, "/records")

	if body := r.Body.String(); len(body) > 0 && contains(body, secret) {
		t.Errorf("応答本文に内部の詳細が含まれている: %q", body)
	}
}

func newHandler(t *testing.T, scope dnsname.Scope, backend provider.Backend) http.Handler {
	t.Helper()
	return webhook.NewHandler(provider.New(scope, backend, discardLogger()))
}

func doGet(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept", mediaType)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	return r
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
