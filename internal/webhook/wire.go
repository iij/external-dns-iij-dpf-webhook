package webhook

import "github.com/iij/external-dns-iij-dpf-webhook/internal/provider"

// 本ファイルは ExternalDNS webhook provider API の転送形式を定義する。
//
// 形は上流仕様 (kubernetes-sigs/external-dns v0.22.0 の api/webhook.yaml) が定める。
// フィールドを足したり名前を変えたりしないこと。独自拡張は原則 I に反する。

// filtersResponse は GET / の応答。上流の schema "filters"。
type filtersResponse struct {
	// Filters は管理対象ドメイン。空でも null にせず [] とする。
	// null は「未指定」と読まれ、全ドメイン許可と解釈されうる (FR-002)。
	Filters []string `json:"filters"`
}

// endpoint は 1 つの DNS レコード。上流の schema "endpoint"。
type endpoint struct {
	DNSName    string   `json:"dnsName"`
	Targets    []string `json:"targets"`
	RecordType string   `json:"recordType"`
	RecordTTL  int64    `json:"recordTTL,omitempty"`

	// SetIdentifier と Labels、ProviderSpecific は上流仕様に含まれるが、
	// 本 provider は解釈しない。受け取った値を保持せず、応答にも含めない。
	// DPF に対応する概念がないため、往復させると存在しない情報を
	// 伝えることになる。
	SetIdentifier    string                     `json:"setIdentifier,omitempty"`
	Labels           map[string]string          `json:"labels,omitempty"`
	ProviderSpecific []providerSpecificProperty `json:"providerSpecific,omitempty"`
}

// providerSpecificProperty は上流の schema "providerSpecificProperty"。
type providerSpecificProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// toEndpoint はドメインのレコードを転送形式へ変換する。
//
// 名前は正規化名のまま出す。境界での変換は行わない (research R7)。
func toEndpoint(r provider.Record) endpoint {
	targets := r.Values
	if targets == nil {
		targets = []string{}
	}
	return endpoint{
		DNSName:    r.Name.String(),
		Targets:    targets,
		RecordType: r.Type.String(),
		RecordTTL:  int64(r.TTL),
	}
}

// toEndpoints はレコードの並びを転送形式へ変換する。
// 空でも null にせず [] を返す。
func toEndpoints(records []provider.Record) []endpoint {
	out := make([]endpoint, 0, len(records))
	for _, r := range records {
		out = append(out, toEndpoint(r))
	}
	return out
}
