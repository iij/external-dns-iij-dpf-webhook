// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// 本ファイルは、ゾーン反映の実行者が DPF の履歴へ記録されることを確かめる
// (004 の FR-001、SC-002)。
//
// **要求に説明を載せることと、DPF が履歴として保持することは別の事実である。**
// 前者は internal/dpf の単体テストで足りる。後者は実環境でしか分からない。
//
// 機械化しない確認が 1 つある。**人手による変更と並べて区別できること
// (004 quickstart 3、SC-001) は機械化しない。** DPF コンソールの操作を伴う
// ためである。省いたのではなく、機械化の対象外として残している。

// attributionSetup は検証用ゾーンに対する provider とクライアントを組み立てる。
//
// 履歴の読み取りは provider.Backend に無い操作であるため、クライアントを
// 直接持つ必要がある (004 contracts)。
func attributionSetup(t *testing.T) (*provider.Provider, *dpf.Client, dnsname.Name) {
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

// TestApplyAttribution は、適用した反映の履歴に記録が残ることを確かめる。
//
// 1 つのテストに束ねているのは、途中で失敗しても後始末が確実に走るようにするため。
func TestApplyAttribution(t *testing.T) {
	p, client, zone := attributionSetup(t)

	zoneObj := zoneByName(t, client, zone)
	name := testName(t, zone)

	rec := func(value string) provider.Record {
		return provider.Record{Name: name, Type: provider.TypeA, TTL: 300, Values: []string{value}}
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		//nolint:errcheck,gosec // 後始末。既に削除済みなら何も起きない
		p.ApplyChanges(ctx, provider.ChangeSet{Delete: []provider.Record{rec("192.0.2.1")}})
	})

	// 適用前の履歴。これが書き換わらないことを後で確かめる (FR-008)。
	before, err := client.ZoneHistories(t.Context(), zoneObj)
	if err != nil {
		t.Fatalf("履歴の取得に失敗: %v", err)
	}
	t.Logf("適用前の履歴件数: %d", len(before))

	t.Run("反映の履歴に記録が残る", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			Create: []provider.Record{rec("192.0.2.1")},
		}); err != nil {
			t.Fatalf("適用に失敗: %v", err)
		}

		got, err := client.ZoneHistories(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("履歴の取得に失敗: %v", err)
		}
		if len(got) == 0 {
			t.Fatal("履歴が空。反映が記録されていない")
		}

		// 最新の履歴が本サービスによる反映であること。
		latest := got[0]
		t.Logf("最新の履歴: committed_at=%s description=%q", latest.CommittedAt, latest.Description)
		if !strings.Contains(latest.Description, "external-dns-iij-dpf-webhook") {
			t.Errorf("最新の履歴の説明 = %q, want external-dns-iij-dpf-webhook を含む",
				latest.Description)
		}
	})

	t.Run("連続した反映のすべてに記録が残る", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			UpdateTo: []provider.Record{rec("192.0.2.2")},
		}); err != nil {
			t.Fatalf("適用に失敗: %v", err)
		}

		got, err := client.ZoneHistories(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("履歴の取得に失敗: %v", err)
		}

		// 本テストが加えた 2 件が、いずれも記録を持つこと (SC-002)。
		added := len(got) - len(before)
		if added < 2 {
			t.Fatalf("増えた履歴 = %d 件, want 2 件以上", added)
		}
		for i := range 2 {
			if !strings.Contains(got[i].Description, "external-dns-iij-dpf-webhook") {
				t.Errorf("履歴[%d] の説明 = %q, want 記録を含む", i, got[i].Description)
			}
		}
	})

	t.Run("既存の履歴が書き換わらない", func(t *testing.T) {
		got, err := client.ZoneHistories(t.Context(), zoneObj)
		if err != nil {
			t.Fatalf("履歴の取得に失敗: %v", err)
		}
		if len(got) < len(before) {
			t.Fatalf("履歴が減った: %d → %d。履歴は追記のみであるべき", len(before), len(got))
		}

		// 適用前に存在した履歴は、末尾側にそのまま残る (新しい順に返るため)。
		offset := len(got) - len(before)
		for i := range before {
			was, now := before[i], got[offset+i]
			if was.ID != now.ID || was.Description != now.Description {
				t.Errorf("適用前の履歴が書き換わった: id=%d %q → id=%d %q",
					was.ID, was.Description, now.ID, now.Description)
			}
		}
	})
}
