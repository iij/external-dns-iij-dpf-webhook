// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"fmt"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

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

// changes は POST /records の要求。上流の schema "changes"。
//
// UpdateOld と UpdateNew は対になる。本 provider は UpdateOld を用いない。
// 適用時点の現在値が UpdateOld と食い違っていても UpdateNew を適用する。
// 一致を要求すると差分が解消せず振動する (SC-007、research R4)。
type changes struct {
	Create    []endpoint `json:"create"`
	UpdateOld []endpoint `json:"updateOld"`
	UpdateNew []endpoint `json:"updateNew"`
	Delete    []endpoint `json:"delete"`
}

// toChangeSet は要求をドメインの変更セットへ変換する。
//
// 名前は受信時に正規化する。上流がどの表現で送っても、以降の処理は
// 正規化名だけを扱う (research R7)。
//
// 解釈できない名前や種別は恒久的な失敗とする。DPF へ送る前に止めることで、
// 無駄な API 呼び出しを避ける。
func toChangeSet(c changes) (provider.ChangeSet, error) {
	create, err := toRecords(c.Create)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	updateTo, err := toRecords(c.UpdateNew)
	if err != nil {
		return provider.ChangeSet{}, err
	}
	del, err := toRecords(c.Delete)
	if err != nil {
		return provider.ChangeSet{}, err
	}

	// UpdateOld は読み取らない。上記の理由により、適用の判断に使わない。
	return provider.ChangeSet{Create: create, UpdateTo: updateTo, Delete: del}, nil
}

func toRecords(endpoints []endpoint) ([]provider.Record, error) {
	out := make([]provider.Record, 0, len(endpoints))
	for _, e := range endpoints {
		r, err := toRecord(e)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func toRecord(e endpoint) (provider.Record, error) {
	name, err := dnsname.Parse(e.DNSName)
	if err != nil {
		return provider.Record{}, fmt.Errorf("%w: 名前を解釈できません: %w", provider.ErrPermanent, err)
	}

	rtype, err := provider.ParseRecordType(e.RecordType)
	if err != nil {
		return provider.Record{}, err
	}

	return provider.Record{
		Name:   name,
		Type:   rtype,
		TTL:    int(e.RecordTTL),
		Values: e.Targets,
	}, nil
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
