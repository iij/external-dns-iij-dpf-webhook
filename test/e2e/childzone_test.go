// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// setupParentChild は親ゾーンと子ゾーンが併存する環境で provider を組み立てる。
//
// 子ゾーン名は DPF_E2E_CHILD_ZONE で与える。**親ゾーンの配下に実在するゾーンで
// あること。** 未設定ならスキップする。誤って本番ゾーンへ向かうより、検証が
// 行われないことが明示される方が安全である。
//
// 範囲は親ゾーンのみを与える。子ゾーンは親の配下にあるため、これで双方が
// 管理対象に入る。
func setupParentChild(t *testing.T) (*provider.Provider, provider.Backend, dnsname.Name, dnsname.Name) {
	t.Helper()

	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	parentName := os.Getenv("DPF_E2E_ZONE")
	childName := os.Getenv("DPF_E2E_CHILD_ZONE")
	if tokenFile == "" || parentName == "" || childName == "" {
		t.Skip("DPF_E2E_TOKEN_FILE / DPF_E2E_ZONE / DPF_E2E_CHILD_ZONE が必要です")
	}

	parent, err := dnsname.Parse(parentName)
	if err != nil {
		t.Fatalf("DPF_E2E_ZONE を解釈できません: %v", err)
	}
	child, err := dnsname.Parse(childName)
	if err != nil {
		t.Fatalf("DPF_E2E_CHILD_ZONE を解釈できません: %v", err)
	}
	if !parent.Contains(child) || parent == child {
		t.Fatalf("DPF_E2E_CHILD_ZONE (%s) は DPF_E2E_ZONE (%s) の配下でなければなりません",
			child, parent)
	}

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	backend, err := dpf.NewClient(ctx, config.DPF{TokenFile: tokenFile}, nil, nil)
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}

	p := provider.New(dnsname.NewScope(parent), backend, slog.New(slog.DiscardHandler))
	return p, backend, parent, child
}

// zoneByName は DPF 上のゾーンを名前で引く。
func zoneByName(t *testing.T, backend provider.Backend, name dnsname.Name) provider.Zone {
	t.Helper()

	zones, err := backend.ListZones(t.Context())
	if err != nil {
		t.Fatalf("ゾーン一覧の取得に失敗: %v", err)
	}
	for _, z := range zones {
		if z.Name == name {
			return z
		}
	}
	t.Fatalf("ゾーン %s が DPF 上に存在しません", name)
	return provider.Zone{}
}

// recordExistsInZone は、指定ゾーンに名前と種別が一致するレコードがあるかを報告する。
//
// provider.Records ではなくゾーン単位の一覧を見る。**どのゾーンに書かれたか**が
// 確かめたいことであり、provider の一覧はゾーンを隠すためである。
func recordExistsInZone(t *testing.T, backend provider.Backend, zone provider.Zone, name dnsname.Name) bool {
	t.Helper()

	records, err := backend.ListRecords(t.Context(), zone)
	if err != nil {
		t.Fatalf("ゾーン %s のレコード取得に失敗: %v", zone.Name, err)
	}
	for _, r := range records {
		if r.Name == name && r.Type == provider.TypeA {
			return true
		}
	}
	return false
}

// TestParentChildZoneRouting は、親子ゾーンが併存するとき最も深いゾーンだけが
// 更新されることを実環境で確かめる (SC-010、FR-040/FR-043)。
//
// 適用先を誤ると、権威を持たない親ゾーンに影のレコードが作られる。名前解決は
// 変わらないまま「成功した」と見えるため、モックでは気付けても実環境の思い違い
// (DPF がどちらのゾーンを受け付けるか) は実際に投げないと分からない。
//
// 1 つのテストに束ねているのは、途中で失敗しても後始末が確実に走るようにするため。
func TestParentChildZoneRouting(t *testing.T) {
	p, backend, parent, child := setupParentChild(t)

	parentZone := zoneByName(t, backend, parent)
	childZone := zoneByName(t, backend, child)

	// 子ゾーン配下の名前。帰属先は子ゾーンでなければならない。
	childName := testName(t, child)
	// 親ゾーン直下の名前。子ゾーンが存在しても親に書かれる。
	parentName := testName(t, parent)

	rec := func(n dnsname.Name) provider.Record {
		return provider.Record{Name: n, Type: provider.TypeA, TTL: 300, Values: []string{"192.0.2.1"}}
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		//nolint:errcheck,gosec // 後始末。既に削除済みなら何も起きない
		p.ApplyChanges(ctx, provider.ChangeSet{
			Delete: []provider.Record{rec(childName), rec(parentName)},
		})
	})

	t.Run("子ゾーン配下の名前は子ゾーンへ書かれる", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			Create: []provider.Record{rec(childName)},
		}); err != nil {
			t.Fatalf("適用に失敗: %v", err)
		}

		if !recordExistsInZone(t, backend, childZone, childName) {
			t.Errorf("%s が子ゾーン %s にない", childName, child)
		}
		if recordExistsInZone(t, backend, parentZone, childName) {
			t.Errorf("%s が親ゾーン %s に書かれた。権威を持たないゾーンに影ができている",
				childName, parent)
		}
	})

	t.Run("親ゾーン直下の名前は親ゾーンへ書かれる", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			Create: []provider.Record{rec(parentName)},
		}); err != nil {
			t.Fatalf("適用に失敗: %v", err)
		}

		if !recordExistsInZone(t, backend, parentZone, parentName) {
			t.Errorf("%s が親ゾーン %s にない", parentName, parent)
		}
	})

	// SC-011: 書き込みと読み取りの往復が閉じる。
	//
	// 子ゾーンへ書いた値が親ゾーン由来として読み戻されたり、両ゾーンから
	// 2 件返ったりすると、ExternalDNS は同じ差分を出し続ける (差分の振動)。
	t.Run("読み戻しが重複しない", func(t *testing.T) {
		records, err := p.Records(t.Context())
		if err != nil {
			t.Fatalf("レコード一覧の取得に失敗: %v", err)
		}

		count := map[dnsname.Name]int{}
		for _, r := range records {
			if r.Type != provider.TypeA {
				continue
			}
			if r.Name == childName || r.Name == parentName {
				count[r.Name]++
			}
		}
		for _, n := range []dnsname.Name{childName, parentName} {
			if count[n] != 1 {
				t.Errorf("%s の件数 = %d, want 1", n, count[n])
			}
		}
	})

	// SC-012: NS は種別として対象外であり、一覧に現れない。
	t.Run("NS が一覧に現れない", func(t *testing.T) {
		records, err := p.Records(t.Context())
		if err != nil {
			t.Fatalf("レコード一覧の取得に失敗: %v", err)
		}
		for _, r := range records {
			if r.Type == "NS" {
				t.Errorf("NS が返った: %s", r.Name)
			}
		}
	})
}
