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

// 本ファイルは、本 provider が書いたレコードのラベルが印のみになることを
// 実環境で確かめる (005 の FR-001、SC-001)。
//
// **投入集合に載せることと、DPF が保存することは別である。** 前者は
// internal/dpf の単体テストで足りる。後者は実環境でしか分からない。
//
// 機械化しない確認が 1 つある。**DPF コンソールから人手でレコードを作り、
// それに印が付かないことを確かめる手順 (005 quickstart 3) は機械化しない。**
// コンソールの操作を伴うためである。省いたのではなく、機械化の対象外として残す。
//
// 一方、**本 provider が触らないレコードのラベルが変わらないこと** (FR-003、
// SC-004) は機械化する。人手のレコードを巻き込まないことが、印の価値の前提で
// あり、provider 側の経路だけで確かめられる。

// labelSetup は検証用ゾーンに対する provider とクライアントを組み立てる。
//
// 印で絞った一覧の取得は provider.Backend に無い操作であるため、クライアントを
// 直接持つ必要がある (005 contracts)。
func labelSetup(t *testing.T) (*provider.Provider, *dpf.Client, dnsname.Name) {
	t.Helper()

	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	zoneName := os.Getenv("DPF_E2E_ZONE")
	if tokenFile == "" || zoneName == "" {
		t.Skip("DPF_E2E_TOKEN_FILE と DPF_E2E_ZONE が必要です。検証用ゾーンでのみ実行してください")
	}

	zone, err := dnsname.Parse(zoneName)
	if err != nil {
		t.Fatalf("DPF_E2E_ZONE を解釈できません: %v", err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	client, err := dpf.NewClient(ctx, config.DPF{TokenFile: tokenFile}, nil, nil)
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}

	p := provider.New(dnsname.NewScope(zone), client, slog.New(slog.DiscardHandler))
	return p, client, zone
}

// TestManagedByLabel は、書いたレコードに印が付き、印で絞り込めることを確かめる。
//
// 1 つのテストに束ねているのは、途中で失敗しても後始末が確実に走るようにするため。
func TestManagedByLabel(t *testing.T) {
	p, client, zone := labelSetup(t)

	zoneObj := zoneByName(t, client, zone)
	name := testName(t, zone)
	txtName := testName(t, zone)

	rec := func(n dnsname.Name, typ provider.RecordType, value string) provider.Record {
		return provider.Record{Name: n, Type: typ, TTL: 300, Values: []string{value}}
	}

	all := []provider.Record{
		rec(name, provider.TypeA, "192.0.2.1"),
		// ExternalDNS の所有権 TXT を模したもの。DPF へ書くのは本 provider である。
		rec(txtName, provider.TypeTXT,
			`"heritage=external-dns,external-dns/owner=e2e,external-dns/resource=ingress/app/x"`),
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		//nolint:errcheck,gosec // 後始末。既に削除済みなら何も起きない
		p.ApplyChanges(ctx, provider.ChangeSet{Delete: all})
	})

	// 適用前に印の付いたレコードを数えておく。増えた分だけを見る。
	before, err := client.RecordsWithManagedBy(t.Context(), zoneObj)
	if err != nil {
		t.Fatalf("印で絞った一覧の取得に失敗: %v", err)
	}
	t.Logf("適用前に印の付いたレコード: %d 件", len(before))

	if err := p.ApplyChanges(t.Context(), provider.ChangeSet{Create: all}); err != nil {
		t.Fatalf("適用に失敗: %v", err)
	}

	t.Run("印で絞った一覧に現れる", func(t *testing.T) {
		got, err := client.RecordsWithManagedBy(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("印で絞った一覧の取得に失敗: %v", err)
		}

		found := map[string]bool{}
		for _, r := range got {
			found[r.Name.String()+" "+r.Type.String()] = true
		}

		for _, want := range all {
			key := want.Name.String() + " " + want.Type.String()
			if !found[key] {
				t.Errorf("%s が印で絞った一覧にない (SC-001、SC-002)", key)
			}
		}
	})

	t.Run("絞った一覧に管理外のレコードが含まれない", func(t *testing.T) {
		got, err := client.RecordsWithManagedBy(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("印で絞った一覧の取得に失敗: %v", err)
		}

		// ゾーンには SOA とゾーン apex の NS が必ずある。これらは本 provider が
		// 管理せず、印も付かない。絞った一覧に現れてはならない (SC-003)。
		for _, r := range got {
			if r.Name == zone && (r.Type == "NS" || r.Type == "SOA") {
				t.Errorf("管理外のレコードが含まれている: %s %s", r.Name, r.Type)
			}
		}
		t.Logf("印の付いたレコード: %d 件", len(got))
	})

	t.Run("触らないレコードのラベルが変わらない", func(t *testing.T) {
		// 2 つのうち 1 つだけを更新し、もう一方が影響を受けないことを見る。
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			UpdateTo: []provider.Record{rec(name, provider.TypeA, "192.0.2.2")},
		}); err != nil {
			t.Fatalf("適用に失敗: %v", err)
		}

		got, err := client.RecordsWithManagedBy(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("印で絞った一覧の取得に失敗: %v", err)
		}

		// 更新していない TXT も、印の付いたままである。
		var seen bool
		for _, r := range got {
			if r.Name == txtName && r.Type == provider.TypeTXT {
				seen = true
			}
		}
		if !seen {
			t.Errorf("更新していない %s TXT が印の付いた一覧から消えた (FR-003)", txtName)
		}
	})
}
