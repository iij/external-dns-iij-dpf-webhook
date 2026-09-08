// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
)

// changesJSON は POST /records の要求形。
// 上流仕様 (api/webhook.yaml v0.22.0) の schema "changes" に対応する。
type changesJSON struct {
	Create    []endpointJSON `json:"create,omitempty"`
	UpdateOld []endpointJSON `json:"updateOld,omitempty"`
	UpdateNew []endpointJSON `json:"updateNew,omitempty"`
	Delete    []endpointJSON `json:"delete,omitempty"`
}

func doPost(t *testing.T, h http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()

	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("要求の符号化に失敗: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Accept", mediaType)
	req.Header.Set("Content-Type", mediaType)
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	return r
}

func testZone(t *testing.T) provider.Zone {
	t.Helper()
	return provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
}

// 成功時の状態コードは 204 No Content。200 ではない。
//
// 上流仕様が明示的にこの値を定めている。200 を返すと ExternalDNS 側の
// 解釈が変わりうる。
func TestPostRecords_SuccessIs204(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doPost(t, h, "/records", changesJSON{
		Create: []endpointJSON{{
			DNSName:    "www.example.jp",
			Targets:    []string{"192.0.2.1"},
			RecordType: "A",
			RecordTTL:  300,
		}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("204 なのに本文がある: %q", rec.Body.String())
	}
}

// 空の変更セットは、何も変更せずに成功とする。
func TestPostRecords_EmptyChangeSetSucceeds(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doPost(t, h, "/records", changesJSON{})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("Apply が %d 回呼ばれた。空の変更セットでは適用しない", applies)
	}
}

// 末尾ドットの有無が異なる名前は、同一のレコードとして扱う。
//
// 受信時に正規化するため、上流がどちらの表現で送っても動作は変わらない
// (research R7)。
func TestPostRecords_TrailingDotIsEquivalent(t *testing.T) {
	t.Parallel()

	scope := dnsname.NewScope(dnsname.MustParse("example.jp"))

	var applied []string
	for _, name := range []string{"www.example.jp", "www.example.jp.", "WWW.Example.JP"} {
		backend := providertest.New().WithZone(testZone(t))
		h := newHandler(t, scope, backend)

		rec := doPost(t, h, "/records", changesJSON{
			Create: []endpointJSON{{
				DNSName:    name,
				Targets:    []string{"192.0.2.1"},
				RecordType: "A",
				RecordTTL:  300,
			}},
		})
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%q で状態コード = %d, want %d\n%s", name, rec.Code, http.StatusNoContent, rec.Body.String())
		}
		if len(backend.Applied) != 1 || len(backend.Applied[0].ChangeSet.Create) != 1 {
			t.Fatalf("%q で適用内容が想定と異なる: %+v", name, backend.Applied)
		}
		applied = append(applied, backend.Applied[0].ChangeSet.Create[0].Name.String())
	}

	for _, got := range applied {
		if got != "www.example.jp." {
			t.Errorf("正規化された名前 = %q, want %q (入力表現に依存してはならない)", got, "www.example.jp.")
		}
	}
}

// updateOld は用いない。updateNew の内容を適用する。
//
// 適用時点の現在値が updateOld と食い違っていても updateNew を適用する。
// 一致を要求すると差分が解消せず振動する (SC-007、research R4)。
func TestPostRecords_UsesUpdateNewNotUpdateOld(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doPost(t, h, "/records", changesJSON{
		UpdateOld: []endpointJSON{{
			DNSName: "www.example.jp", Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300,
		}},
		UpdateNew: []endpointJSON{{
			DNSName: "www.example.jp", Targets: []string{"192.0.2.99"}, RecordType: "A", RecordTTL: 60,
		}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if len(backend.Applied) != 1 {
		t.Fatalf("適用が 1 回でない: %+v", backend.Applied)
	}

	cs := backend.Applied[0].ChangeSet
	if len(cs.UpdateTo) != 1 {
		t.Fatalf("UpdateTo = %+v, want 1 件", cs.UpdateTo)
	}
	if cs.UpdateTo[0].Values[0] != "192.0.2.99" {
		t.Errorf("適用値 = %q, want %q (updateNew を使うこと)", cs.UpdateTo[0].Values[0], "192.0.2.99")
	}
	if cs.UpdateTo[0].TTL != 60 {
		t.Errorf("適用 TTL = %d, want 60", cs.UpdateTo[0].TTL)
	}
}

// 解釈できない要求本文は恒久的な失敗として返す。
func TestPostRecords_MalformedBodyIs4xx(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	req := httptest.NewRequest(http.MethodPost, "/records", bytes.NewReader([]byte("{ this is not json")))
	req.Header.Set("Accept", mediaType)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("状態コード = %d, want 4xx", rec.Code)
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("解釈に失敗したのに Apply が %d 回呼ばれた", applies)
	}
}

// FR-002: 管理対象が空なら、変更を一切適用しない。
func TestPostRecords_EmptyScopeAppliesNothing(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(), backend)

	rec := doPost(t, h, "/records", changesJSON{
		Create: []endpointJSON{{
			DNSName: "www.example.jp", Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300,
		}},
	})

	if rec.Code != http.StatusNoContent {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusNoContent, rec.Body.String())
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("管理対象が空なのに Apply が %d 回呼ばれた", applies)
	}
}
