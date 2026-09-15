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

	// NS は種別ごと管理対象外であり、恒久的な失敗になる (FR-029)。
	cs := provider.ChangeSet{
		Delete: []provider.Record{rec("example.jp", "NS", 3600, "ns1.example.jp.")},
	}

	err := p.ApplyChanges(t.Context(), cs)
	if err == nil {
		t.Fatal("NS の削除が受け入れられた")
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

// zones3 は親・子・孫の 3 ゾーンを返す。
func zones3() (parent, child, grandchild provider.Zone) {
	return provider.Zone{Name: dnsname.MustParse("example.jp"), ID: "z1"},
		provider.Zone{Name: dnsname.MustParse("sub.example.jp"), ID: "z2"},
		provider.Zone{Name: dnsname.MustParse("a.sub.example.jp"), ID: "z3"}
}

// appliedTo は zone へ適用された変更セットを集める。
func appliedTo(backend *providertest.Backend, id string) []provider.ChangeSet {
	var out []provider.ChangeSet
	for _, a := range backend.Applied {
		if a.Zone.ID == id {
			out = append(out, a.ChangeSet)
		}
	}
	return out
}

// FR-040: 親・子・孫が併存するとき、最も深く一致するゾーンだけが更新される。
//
// 適用先を誤ると、権威を持たない親ゾーンに影のレコードが作られる。名前解決は
// 変わらないまま「成功した」と見えるため、運用者から最も気付きにくい。
func TestApply_RoutesToDeepestZone(t *testing.T) {
	t.Parallel()

	parent, child, grandchild := zones3()

	cases := []struct {
		name     string
		wantZone string
	}{
		{"www.example.jp", "z1"},
		{"www.sub.example.jp", "z2"},
		{"www.a.sub.example.jp", "z3"},
	}

	for _, c := range cases {
		backend := providertest.New().WithZone(parent).WithZone(child).WithZone(grandchild)
		p := provider.New(scopeJP(), backend, discardLogger())

		cs := provider.ChangeSet{
			Create: []provider.Record{rec(c.name, provider.TypeA, 300, "192.0.2.1")},
		}
		if err := p.ApplyChanges(t.Context(), cs); err != nil {
			t.Fatalf("%s: ApplyChanges = error %v", c.name, err)
		}

		if len(backend.Applied) != 1 {
			t.Fatalf("%s: 適用ゾーン数 = %d, want 1", c.name, len(backend.Applied))
		}
		if got := backend.Applied[0].Zone.ID; got != c.wantZone {
			t.Errorf("%s: 適用先 = %s, want %s", c.name, got, c.wantZone)
		}
	}
}

// 孫ゾーンが存在しなければ、その名前は親ゾーンへ書かれる。
func TestApply_FallsBackWhenChildZoneAbsent(t *testing.T) {
	t.Parallel()

	parent, child, _ := zones3()

	backend := providertest.New().WithZone(parent).WithZone(child)
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.a.sub.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}
	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("ApplyChanges = error %v", err)
	}

	if len(backend.Applied) != 1 {
		t.Fatalf("適用ゾーン数 = %d, want 1", len(backend.Applied))
	}
	if got := backend.Applied[0].Zone.ID; got != "z2" {
		t.Errorf("適用先 = %s, want z2 (最も深く一致する sub.example.jp)", got)
	}
}

// FR-042: 1 つの変更セットが複数ゾーンにまたがるとき、ゾーンごとに適用する。
func TestApply_SplitsAcrossZones(t *testing.T) {
	t.Parallel()

	parent, child, grandchild := zones3()

	backend := providertest.New().WithZone(parent).WithZone(child).WithZone(grandchild)
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{
			rec("www.example.jp", provider.TypeA, 300, "192.0.2.1"),
			rec("www.sub.example.jp", provider.TypeA, 300, "192.0.2.2"),
			rec("www.a.sub.example.jp", provider.TypeA, 300, "192.0.2.3"),
		},
	}
	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("ApplyChanges = error %v", err)
	}

	if len(backend.Applied) != 3 {
		t.Fatalf("適用ゾーン数 = %d, want 3", len(backend.Applied))
	}
	for _, id := range []string{"z1", "z2", "z3"} {
		got := appliedTo(backend, id)
		if len(got) != 1 {
			t.Errorf("ゾーン %s への適用回数 = %d, want 1", id, len(got))
			continue
		}
		if len(got[0].Create) != 1 {
			t.Errorf("ゾーン %s の作成件数 = %d, want 1", id, len(got[0].Create))
		}
	}
}

// FR-043: あるゾーンへの適用が、他のゾーンの内容に及ばない。
//
// 子ゾーンに属する名前のレコードを親ゾーンへ作らない。
func TestApply_DoesNotWriteChildNamesToParentZone(t *testing.T) {
	t.Parallel()

	parent, child, _ := zones3()

	backend := providertest.New().WithZone(parent).WithZone(child)
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.sub.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}
	if err := p.ApplyChanges(t.Context(), cs); err != nil {
		t.Fatalf("ApplyChanges = error %v", err)
	}

	if got := appliedTo(backend, "z1"); len(got) != 0 {
		t.Errorf("親ゾーンへ %d 件適用された。子ゾーンに属する名前は親へ書かない", len(got))
	}
	if got := appliedTo(backend, "z2"); len(got) != 1 {
		t.Errorf("子ゾーンへの適用回数 = %d, want 1", len(got))
	}
}

// FR-041: 含むゾーンがなければ恒久的な失敗。より浅いゾーンへ倒さない。
func TestApply_NoOwningZoneIsPermanentFailure(t *testing.T) {
	t.Parallel()

	// 管理対象範囲には含まれるが、DPF 上に対応するゾーンがない。
	backend := providertest.New().WithZone(
		provider.Zone{Name: dnsname.MustParse("other.example.jp"), ID: "z9"})
	p := provider.New(scopeJP(), backend, discardLogger())

	cs := provider.ChangeSet{
		Create: []provider.Record{rec("www.example.jp", provider.TypeA, 300, "192.0.2.1")},
	}

	err := p.ApplyChanges(t.Context(), cs)
	if err == nil {
		t.Fatal("帰属先のないレコードが受け入れられた")
	}
	if !errors.Is(err, provider.ErrPermanent) {
		t.Errorf("err = %v, want ErrPermanent", err)
	}
	if _, _, applies := backend.Counts(); applies != 0 {
		t.Errorf("解決できないのに Apply が %d 回呼ばれた", applies)
	}
}

// SC-011 / FR-044: 書き込みと読み取りの往復が閉じる。
//
// 子ゾーンへ書いた値が親ゾーン由来として読み戻されたり、両ゾーンから 2 件
// 返ったりすると、ExternalDNS は「まだ差分がある」と判断して同じ変更を出し
// 続ける (差分の振動)。帰属の規則が 1 つしかないことで、これが起きない。
func TestApply_ReadBackIsStableAcrossZones(t *testing.T) {
	t.Parallel()

	parent, child, grandchild := zones3()

	backend := providertest.New().WithZone(parent).WithZone(child).WithZone(grandchild)
	p := provider.New(scopeJP(), backend, discardLogger())

	written := rec("www.a.sub.example.jp", provider.TypeA, 300, "192.0.2.3")
	if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
		Create: []provider.Record{written},
	}); err != nil {
		t.Fatalf("ApplyChanges = error %v", err)
	}

	// 適用先のゾーンへ、書いた内容が入ったものとして読み戻す。
	applied := backend.Applied[0]
	backend.Records[applied.Zone.Name.String()] = applied.ChangeSet.Create

	got, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("Records = error %v", err)
	}

	if len(got) != 1 {
		t.Fatalf("読み戻し件数 = %d, want 1: %v", len(got), names(got))
	}
	if got[0].Name != written.Name || got[0].Type != written.Type {
		t.Errorf("読み戻し = %s %s, want %s %s",
			got[0].Name, got[0].Type, written.Name, written.Type)
	}
}
