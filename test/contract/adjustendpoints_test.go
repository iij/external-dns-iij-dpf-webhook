// SPDX-License-Identifier: Apache-2.0

package contract

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
)

// 成功時は 200 で、調整後のレコード配列を返す。
func TestAdjustEndpoints_ReturnsAdjusted(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())

	rec := doPost(t, h, "/adjustendpoints", []endpointJSON{{
		DNSName:    "www.example.jp",
		Targets:    []string{"192.0.2.1"},
		RecordType: "A",
		RecordTTL:  300,
	}})

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != mediaType {
		t.Errorf("Content-Type = %q, want %q", got, mediaType)
	}

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].DNSName != "www.example.jp." {
		t.Errorf("dnsName = %q, want %q (正規化名で返す)", got[0].DNSName, "www.example.jp.")
	}
}

// 調整の必要がない入力は、そのまま返る。
func TestAdjustEndpoints_UnchangedInputPassesThrough(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())

	in := []endpointJSON{{
		DNSName:    "www.example.jp.",
		Targets:    []string{"192.0.2.1"},
		RecordType: "A",
		RecordTTL:  300,
	}}
	rec := doPost(t, h, "/adjustendpoints", in)

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答を解釈できない: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].RecordTTL != 300 || got[0].Targets[0] != "192.0.2.1" {
		t.Errorf("内容が変化した: %+v", got[0])
	}
}

// FR-015: 調整は冪等。調整結果を再度調整に掛けても変わらない。
//
// 冪等でないと、ExternalDNS が同じ差分を検出し続ける (SC-007)。
func TestAdjustEndpoints_Idempotent(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())

	in := []endpointJSON{
		{DNSName: "WWW.Example.JP", Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 1 << 40},
		{DNSName: "t.example.jp", Targets: []string{`"` + strings.Repeat("a", 600) + `"`}, RecordType: "TXT", RecordTTL: 300},
	}

	first := adjustRoundTrip(t, h, in)
	second := adjustRoundTrip(t, h, first)

	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Errorf("再調整で内容が変化した。冪等でなければならない\n1 回目: %s\n2 回目: %s", firstJSON, secondJSON)
	}
}

// 空の配列は空で返る。
func TestAdjustEndpoints_Empty(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())

	rec := doPost(t, h, "/adjustendpoints", []endpointJSON{})
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := strings.TrimSpace(rec.Body.String()); body == "null" {
		t.Error("応答が null。空配列を返すこと")
	}
}

// 調整は DPF に触れない。表現を整えるだけの操作である。
func TestAdjustEndpoints_DoesNotTouchBackend(t *testing.T) {
	t.Parallel()

	backend := providertest.New()
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	doPost(t, h, "/adjustendpoints", []endpointJSON{{
		DNSName: "www.example.jp", Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300,
	}})

	zones, records, applies := backend.Counts()
	if zones != 0 || records != 0 || applies != 0 {
		t.Errorf("バックエンドが呼ばれた (zones=%d, records=%d, applies=%d)", zones, records, applies)
	}
}

func adjustRoundTrip(t *testing.T, h http.Handler, in []endpointJSON) []endpointJSON {
	t.Helper()

	rec := doPost(t, h, "/adjustendpoints", in)
	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var out []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("応答を解釈できない: %v", err)
	}
	return out
}

// FR-029: NS を含む要求は adjustendpoints でも恒久的な失敗として返す。
//
// 変換は POST /records と同じ経路 (toRecords) を通る。片方だけ通してしまうと、
// 調整では受け付けたものが適用で拒否されることになり、ExternalDNS から見て
// 挙動が一貫しない。
func TestAdjustEndpoints_NSIs4xx(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(testZone(t))
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doPost(t, h, "/adjustendpoints", []endpointJSON{{
		DNSName: "sub.example.jp", Targets: []string{"ns1.example.jp."}, RecordType: "NS", RecordTTL: 3600,
	}})

	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("状態コード = %d, want 4xx\n%s", rec.Code, rec.Body.String())
	}
}
