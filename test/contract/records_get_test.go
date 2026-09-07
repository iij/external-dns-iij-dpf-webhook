package contract

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
)

// endpointJSON は GET /records の応答要素。
// 上流仕様 (api/webhook.yaml v0.22.0) の schema "endpoint" に対応する。
type endpointJSON struct {
	DNSName    string   `json:"dnsName"`
	Targets    []string `json:"targets"`
	RecordType string   `json:"recordType"`
	RecordTTL  int64    `json:"recordTTL"`
}

func discardLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}

// 成功時は 200 で、レコードの配列を返す。
func TestGetRecords_ReturnsEndpoints(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().WithZone(zone,
		provider.Record{
			Name:   dnsname.MustParse("www.example.jp"),
			Type:   provider.TypeA,
			TTL:    300,
			Values: []string{"192.0.2.1", "192.0.2.2"},
		},
	)
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doGet(t, h, "/records")

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d\n%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("応答が endpoint の配列として解釈できない: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("レコード件数 = %d, want 1: %+v", len(got), got)
	}

	e := got[0]
	if e.RecordType != "A" {
		t.Errorf("recordType = %q, want %q", e.RecordType, "A")
	}
	if e.RecordTTL != 300 {
		t.Errorf("recordTTL = %d, want 300", e.RecordTTL)
	}
	if len(e.Targets) != 2 {
		t.Errorf("targets = %v, want 2 件", e.Targets)
	}
}

// research R7: 名前は正規化名 (小文字・末尾ドット) のまま返す。
func TestGetRecords_NamesAreCanonical(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().WithZone(zone,
		provider.Record{
			Name:   dnsname.MustParse("WWW.Example.JP"),
			Type:   provider.TypeA,
			TTL:    60,
			Values: []string{"192.0.2.1"},
		},
	)
	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), backend)

	rec := doGet(t, h, "/records")

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解釈できない: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 1 {
		t.Fatalf("件数 = %d, want 1", len(got))
	}
	if got[0].DNSName != "www.example.jp." {
		t.Errorf("dnsName = %q, want %q", got[0].DNSName, "www.example.jp.")
	}
}

// レコードが 1 件もない場合も、空配列を返す。null にしない。
//
// null を返すと受け取り側の解釈が分かれる。空配列は「管理対象だが 0 件」を
// 明確に表す。
func TestGetRecords_EmptyIsArrayNotNull(t *testing.T) {
	t.Parallel()

	h := newHandler(t, dnsname.NewScope(dnsname.MustParse("example.jp")), providertest.New())

	rec := doGet(t, h, "/records")

	if rec.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body == "null\n" || body == "null" {
		t.Errorf("応答が null。空配列を返すこと: %q", body)
	}

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("配列として解釈できない: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0", len(got))
	}
}

// FR-002: 管理対象が空なら、レコードは 1 件も返らない。
//
// 空の範囲で全ゾーンのレコードが返ると、ExternalDNS がそれらを管理下と
// 見なし、次の適用で削除しにいく。
func TestGetRecords_EmptyScopeReturnsNothing(t *testing.T) {
	t.Parallel()

	zone := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	backend := providertest.New().WithZone(zone,
		provider.Record{
			Name:   dnsname.MustParse("www.example.jp"),
			Type:   provider.TypeA,
			TTL:    60,
			Values: []string{"192.0.2.1"},
		},
	)
	h := newHandler(t, dnsname.NewScope(), backend)

	rec := doGet(t, h, "/records")

	var got []endpointJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("解釈できない: %v\n%s", err, rec.Body.String())
	}
	if len(got) != 0 {
		t.Errorf("件数 = %d, want 0。管理対象が空なら何も返してはならない: %+v", len(got), got)
	}

	// バックエンドへの問い合わせも起きないこと。管理対象がないのに
	// DPF を叩くのは無駄であり、レート制限を消費する。
	if _, records, _ := backend.Counts(); records != 0 {
		t.Errorf("ListRecords が %d 回呼ばれた。管理対象が空なら問い合わせない", records)
	}
}
