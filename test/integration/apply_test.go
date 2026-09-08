// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"errors"
	"net/http"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider/providertest"
)

func scopeJP() dnsname.Scope { return dnsname.NewScope(dnsname.MustParse("example.jp")) }

func zoneJP() provider.Zone {
	return provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
}

// FR-010 / SC-002: 同一の変更セットを 10 回適用しても、最終状態が変わらない。
//
// provider 層では「同じ変更セットが同じ内容で境界に渡ること」を確かめる。
// DPF 側の冪等性は merge の性質として internal/dpf で検証する。
func TestApply_IdempotentAcrossRepeats(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}

	for i := range 10 {
		if err := p.ApplyChanges(t.Context(), cs); err != nil {
			t.Fatalf("%d 回目の適用に失敗: %v", i+1, err)
		}
	}

	if len(backend.Applied) != 10 {
		t.Fatalf("適用回数 = %d, want 10", len(backend.Applied))
	}
	first := backend.Applied[0].ChangeSet
	for i, a := range backend.Applied {
		if len(a.ChangeSet.Create) != len(first.Create) {
			t.Errorf("%d 回目の変更セットが 1 回目と異なる", i+1)
		}
		if len(a.ChangeSet.Create) == 1 && a.ChangeSet.Create[0].Name != first.Create[0].Name {
			t.Errorf("%d 回目の対象が 1 回目と異なる", i+1)
		}
	}
}

// FR-009 / SC-004: 管理対象外のレコードは変更・削除されない。
//
// 範囲外は失敗ではなく除外として扱う。失敗にすると、範囲外が 1 件混ざった
// だけで正当な変更まで巻き添えで止まる。
func TestApply_ExcludesOutOfScopeRecords(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{
			rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"),
			rec("www.other.jp", provider.TypeA, 300, "198.51.100.1"),
			rec("www.evil-example.jp", provider.TypeA, 300, "198.51.100.2"),
		},
	}

	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("適用に失敗: %v", err)
	}

	if len(backend.Applied) != 1 {
		t.Fatalf("適用回数 = %d, want 1: %+v", len(backend.Applied), backend.Applied)
	}
	applied := backend.Applied[0].ChangeSet
	if len(applied.Create) != 1 {
		t.Fatalf("適用対象 = %d 件, want 1: %+v", len(applied.Create), applied.Create)
	}
	if applied.Create[0].Name.String() != "www.example.jp." {
		t.Errorf("適用対象 = %q, want %q", applied.Create[0].Name, "www.example.jp.")
	}
}

// 範囲外だけの変更セットは、バックエンドを呼ばずに成功する。
func TestApply_OnlyOutOfScopeDoesNotCallBackend(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.other.jp", provider.TypeA, 300, "198.51.100.1")},
	}

	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("適用に失敗: %v", err)
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("Apply が %d 回呼ばれた。範囲外のみなら呼ばない", applies)
	}
}

// ゾーンをまたぐ変更は、ゾーンごとに分けて適用される。
//
// 一括更新はゾーン単位の操作であるため、まとめて 1 回では適用できない。
func TestApply_SplitsByZone(t *testing.T) {
	t.Parallel()

	z1 := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	z2 := provider.Zone{Name: dnsname.MustParse("example.com"), ID: "z2"}
	backend := providertest.New().WithZone(z1).WithZone(z2)

	scope := dnsname.NewScope(dnsname.MustParse("example.jp"), dnsname.MustParse("example.com"))
	p := provider.New(scope, backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{
			rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"),
			rec("www.example.com", provider.TypeA, 300, "192.0.2.2"),
		},
	}

	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("適用に失敗: %v", err)
	}
	if len(backend.Applied) != 2 {
		t.Fatalf("適用回数 = %d, want 2 (ゾーンごと): %+v", len(backend.Applied), backend.Applied)
	}
}

// FR-012: 途中で失敗したら成功を返さない。
func TestApply_FailurePropagates(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	backend.ApplyErr = errors.Join(provider.ErrTemporary, errors.New("反映に失敗"))
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}

	err := p.ApplyChanges(t.Context(), cs)
	if err == nil {
		t.Fatal("適用が失敗したのに成功が返った")
	}
	if !errors.Is(err, provider.ErrTemporary) {
		t.Errorf("err = %v, want ErrTemporary", err)
	}
}

// 複数ゾーンのうち 1 つが失敗したら、全体として失敗を返す。
//
// 一部だけ成功した状態を成功として返すと、ExternalDNS は失敗した側の
// 変更を反映済みと見なす。
func TestApply_PartialZoneFailureIsFailure(t *testing.T) {
	t.Parallel()

	z1 := provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"}
	z2 := provider.Zone{Name: dnsname.MustParse("example.com"), ID: "z2"}
	backend := providertest.New().WithZone(z1).WithZone(z2)
	backend.ApplyErr = errors.Join(provider.ErrTemporary, errors.New("反映に失敗"))

	scope := dnsname.NewScope(dnsname.MustParse("example.jp"), dnsname.MustParse("example.com"))
	p := provider.New(scope, backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{
			rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"),
			rec("www.example.com", provider.TypeA, 300, "192.0.2.2"),
		},
	}

	if err := p.ApplyChanges(t.Context(), cs); err == nil {
		t.Fatal("一部のゾーンが失敗したのに成功が返った")
	}
}

// 検証に失敗した変更セットは、バックエンドに渡さない。
//
// DPF へ送る前に止めることで、無駄な API 呼び出しとレート制限の消費を避ける。
func TestApply_ValidationFailureDoesNotReachBackend(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	p := provider.New(scopeJP(), backend, discardLogger())

	// apex NS の削除は恒久的な失敗 (FR-029)。
	cs := provider.ChangeSet{
		Delete: []provider.Record{rec("example.jp", provider.TypeNS, 3600, "ns1.example.jp.")},
	}

	err := p.ApplyChanges(t.Context(), cs)
	if err == nil {
		t.Fatal("apex NS の削除が受け入れられた")
	}
	if !errors.Is(err, provider.ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("検証に失敗したのに Apply が %d 回呼ばれた", applies)
	}
}

// 形式違反はバックエンドの状態によらず恒久的な失敗として返す。
//
// 検証をバックエンド呼び出しより後に置くと、DPF が落ちている間は形式違反が
// 5xx になる。ExternalDNS はそれを一時的な障害と解釈して再試行し続けるが、
// 形式違反は何度送っても通らない。
func TestApply_ValidationPrecedesBackendCalls(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	// DPF が応答しない状況を模す。
	backend.ListZonesErr = errors.Join(provider.ErrTemporary, errors.New("DPF が応答しません"))
	p := provider.New(scopeJP(), backend, discardLogger())

	cases := []struct {
		name string
		cs   provider.ChangeSet
	}{
		{"未対応種別", provider.ChangeSet{
			Create: []provider.Record{rec("d.example.jp", provider.RecordType("DNAME"), 300, "t.example.jp.")},
		}},
		{"A の名前にアンダースコア", provider.ChangeSet{
			Create: []provider.Record{rec("_x.example.jp", provider.TypeA, 300, "192.0.2.1")},
		}},
		{"CNAME に複数値", provider.ChangeSet{
			Create: []provider.Record{rec("c.example.jp", provider.TypeCNAME, 300, "a.example.jp.", "b.example.jp.")},
		}},
		{"TTL が範囲外", provider.ChangeSet{
			Create: []provider.Record{rec("w.example.jp", provider.TypeA, 1<<40, "192.0.2.1")},
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := p.ApplyChanges(t.Context(), c.cs)
			if err == nil {
				t.Fatal("形式違反が受け入れられた")
			}
			if errors.Is(err, provider.ErrTemporary) {
				t.Errorf("形式違反が一時的な障害として返された。再試行しても通らない: %v", err)
			}
			if !errors.Is(err, provider.ErrPermanent) && !errors.Is(err, provider.ErrUnsupportedType) {
				t.Errorf("err = %v, want 恒久的な失敗", err)
			}
		})
	}

	// 検証で止まるため、バックエンドには到達しない。
	if zones, _, applies := backend.Counts(); zones != 0 || applies != 0 {
		t.Errorf("バックエンドが呼ばれた (ListZones=%d, Apply=%d)。検証はその前に行う", zones, applies)
	}
}

// 書き込み先のゾーンを解決できない場合は恒久的な失敗。
func TestApply_UnresolvableZoneIsPermanent(t *testing.T) {
	t.Parallel()

	// 範囲には含まれるが、DPF 上にゾーンが存在しない。
	backend := providertest.New()
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}

	err := p.ApplyChanges(t.Context(), cs)
	if err == nil {
		t.Fatal("ゾーンが存在しないのに成功が返った")
	}
	if !errors.Is(err, provider.ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
}

// 空の変更セットはバックエンドを呼ばずに成功する。
func TestApply_EmptyChangeSetSucceeds(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP())
	p := provider.New(scopeJP(), backend, discardLogger())

	if err := p.ApplyChanges(t.Context(), provider.ChangeSet{}); err != nil {
		t.Fatalf("空の変更セットで失敗した: %v", err)
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("Apply が %d 回呼ばれた", applies)
	}
}

// research R4: ロックは適用の内側に閉じる。取得要求だけではロックを取らない。
//
// webhook のレコード取得と適用は独立した HTTP 要求であり、取得の後に適用が
// 来る保証がない。取得側でロックを取ると、適用が来ないままロックが残留して
// ゾーンが操作不能になる。
func TestListRecords_DoesNotApply(t *testing.T) {
	t.Parallel()

	backend := providertest.New().WithZone(zoneJP(),
		rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"))
	h := newHandler(t, scopeJP(), backend)

	r := doGet(t, h, "/records")
	if r.Code != http.StatusOK {
		t.Fatalf("状態コード = %d, want %d", r.Code, http.StatusOK)
	}

	// 取得は適用を伴わない。ロックの取得も書き込みも発生しない。
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("レコード取得で Apply が %d 回呼ばれた", applies)
	}
}
