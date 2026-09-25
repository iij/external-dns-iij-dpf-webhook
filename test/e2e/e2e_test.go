// SPDX-License-Identifier: Apache-2.0

// Package e2e は実際の DPF に対して provider の振る舞いを検証する。
//
// constitution v2.1.0 は、`main` へマージする前にレコードの追加・変更・削除を
// CI で実行することを MUST とする。モックに対するテストは変換と分岐の
// 正しさを示すが、DPF が実際に何を受け付けるかは示さない。
//
// **破壊的操作を行う。** 検証用ゾーンでのみ実行すること。必要な環境変数が
// 揃っていない場合はスキップし、誤って本番ゾーンへ向かないようにする。
//
//	DPF_E2E_TOKEN_FILE  DPF アクセストークンを収めたファイル
//	DPF_E2E_ZONE        検証用ゾーン名 (例: e2e.example.jp)
package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// testTimeout は 1 つの検証に許す時間。反映の待ち合わせを含む。
const testTimeout = 10 * time.Minute

// setup は検証用ゾーンに対する provider を組み立てる。
//
// 環境変数が揃っていなければスキップする。誤って本番ゾーンへ向かうより、
// 検証が行われないことが明示される方が安全である。
func setup(t *testing.T) (*provider.Provider, dnsname.Name) {
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

	backend, err := dpf.NewClient(ctx, config.DPF{TokenFile: tokenFile}, nil, nil)
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}

	return provider.New(dnsname.NewScope(zone), backend, slog.New(slog.DiscardHandler)), zone
}

// testName は検証に使う一意な名前を返す。
//
// 実行ごとに変えることで、前回の失敗が残したレコードと衝突しない。
func testName(t *testing.T, zone dnsname.Name) dnsname.Name {
	t.Helper()

	n, err := dnsname.Parse(fmt.Sprintf("e2e-%d.%s", time.Now().UnixNano(), zone))
	if err != nil {
		t.Fatalf("検証用の名前を作れません: %v", err)
	}
	return n
}

// findRecord は現在のレコード一覧から対象を探す。
func findRecord(t *testing.T, p *provider.Provider, name dnsname.Name) (provider.Record, bool) {
	t.Helper()

	records, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("レコード一覧の取得に失敗: %v", err)
	}
	for _, r := range records {
		if r.Name == name && r.Type == provider.TypeA {
			return r, true
		}
	}
	return provider.Record{}, false
}

// TestLifecycle は追加・変更・削除が実際に反映されることを確かめる
// (constitution v2.1.0)。
//
// 1 つのテストに束ねているのは、途中で失敗しても後始末が確実に走るようにするため。
// 分割すると、追加だけ成功して削除されないレコードが検証用ゾーンに残る。
func TestLifecycle(t *testing.T) {
	p, zone := setup(t)
	name := testName(t, zone)

	rec := func(values ...string) provider.Record {
		return provider.Record{Name: name, Type: provider.TypeA, TTL: 300, Values: values}
	}

	// 途中でどこで失敗しても、レコードを残さない。
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		//nolint:errcheck,gosec // 後始末。既に削除済みなら何も起きない
		p.ApplyChanges(ctx, provider.ChangeSet{Delete: []provider.Record{rec("192.0.2.1")}})
	})

	t.Run("追加", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			Create: []provider.Record{rec("192.0.2.1")},
		}); err != nil {
			t.Fatalf("追加に失敗: %v", err)
		}

		got, ok := findRecord(t, p, name)
		if !ok {
			t.Fatalf("%s が反映されていない", name)
		}
		if !slices.Contains(got.Values, "192.0.2.1") {
			t.Errorf("値 = %v, want 192.0.2.1 を含む", got.Values)
		}
	})

	t.Run("変更", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			UpdateTo: []provider.Record{rec("192.0.2.99")},
		}); err != nil {
			t.Fatalf("変更に失敗: %v", err)
		}

		got, ok := findRecord(t, p, name)
		if !ok {
			t.Fatalf("%s が消えた", name)
		}
		if !slices.Contains(got.Values, "192.0.2.99") {
			t.Errorf("値 = %v, want 192.0.2.99 を含む", got.Values)
		}
		if slices.Contains(got.Values, "192.0.2.1") {
			t.Errorf("値 = %v。変更前の値が残っている", got.Values)
		}
	})

	// FR-010: 同一の変更を再適用しても最終状態が変わらない。
	t.Run("再適用で状態が変わらない", func(t *testing.T) {
		before, _ := findRecord(t, p, name)

		for i := range 3 {
			if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
				UpdateTo: []provider.Record{rec("192.0.2.99")},
			}); err != nil {
				t.Fatalf("%d 回目の再適用に失敗: %v", i+1, err)
			}
		}

		after, ok := findRecord(t, p, name)
		if !ok {
			t.Fatal("再適用でレコードが消えた")
		}
		if !slices.Equal(before.Values, after.Values) || before.TTL != after.TTL {
			t.Errorf("再適用で状態が変わった: %v(TTL %d) → %v(TTL %d)",
				before.Values, before.TTL, after.Values, after.TTL)
		}
	})

	t.Run("削除", func(t *testing.T) {
		if err := p.ApplyChanges(t.Context(), provider.ChangeSet{
			Delete: []provider.Record{rec("192.0.2.99")},
		}); err != nil {
			t.Fatalf("削除に失敗: %v", err)
		}

		if _, ok := findRecord(t, p, name); ok {
			t.Errorf("%s が削除されていない", name)
		}
	})
}

// TestUnmanagedRecordsUntouched は、適用がゾーン内の他のレコードを
// 変更しないことを確かめる (FR-009、SC-004)。
//
// 一括更新方式のため、ここが崩れるとゾーン全体を壊す。実環境で
// 確かめる価値が最も高い性質である。
func TestUnmanagedRecordsUntouched(t *testing.T) {
	p, zone := setup(t)
	name := testName(t, zone)

	before, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("事前のレコード一覧の取得に失敗: %v", err)
	}

	rec := provider.Record{Name: name, Type: provider.TypeA, TTL: 300, Values: []string{"192.0.2.1"}}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		//nolint:errcheck,gosec // 後始末
		p.ApplyChanges(ctx, provider.ChangeSet{Delete: []provider.Record{rec}})
	})

	if applyErr := p.ApplyChanges(t.Context(), provider.ChangeSet{
		Create: []provider.Record{rec},
	}); applyErr != nil {
		t.Fatalf("追加に失敗: %v", applyErr)
	}

	after, err := p.Records(t.Context())
	if err != nil {
		t.Fatalf("事後のレコード一覧の取得に失敗: %v", err)
	}

	// 追加した 1 件を除き、事前と事後で内容が一致すること。
	index := func(records []provider.Record) map[string]provider.Record {
		m := make(map[string]provider.Record, len(records))
		for _, r := range records {
			m[r.Key().String()] = r
		}
		return m
	}
	b, a := index(before), index(after)

	for k, want := range b {
		got, ok := a[k]
		if !ok {
			t.Errorf("%s が消えた。適用対象外のレコードを削除している", k)
			continue
		}
		if !slices.Equal(want.Values, got.Values) || want.TTL != got.TTL {
			t.Errorf("%s が変化した: %v(TTL %d) → %v(TTL %d)",
				k, want.Values, want.TTL, got.Values, got.TTL)
		}
	}

	if len(a) != len(b)+1 {
		t.Errorf("レコード件数 = %d, want %d (追加した 1 件のみ増える)", len(a), len(b)+1)
	}
}
