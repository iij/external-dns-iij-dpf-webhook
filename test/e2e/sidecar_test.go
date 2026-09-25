// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// 本ファイルは ExternalDNS サイドカー構成の検証で使う待ち合わせを担う
// (T083、SC-001)。
//
// クラスタの操作 (kind の起動、チャートの導入、Ingress の作成と削除) は
// ワークフロー側で行う。ここは **DPF 側の状態が期待どおりになるまで待つ**
// 役だけを持つ。両者を 1 つのテストに混ぜると、kubectl の呼び出しが
// テストコードに入り込み、失敗したときにクラスタ側と DPF 側の
// どちらの問題か切り分けにくくなる。
//
//	DPF_E2E_TOKEN_FILE  DPF アクセストークンを収めたファイル
//	DPF_E2E_ZONE        検証用ゾーン名
//	DPF_E2E_WAIT_NAME   待ち合わせる名前 (これがある場合のみ実行)
//	DPF_E2E_WAIT_TYPE   種別 (既定 A)
//	DPF_E2E_WAIT_STATE  "present" または "absent"
//	DPF_E2E_WAIT_BUDGET 待つ時間 (既定 5m。SC-001 の上限)

// defaultWaitBudget は SC-001 が定める上限。
//
// 「Ingress を作成してから DPF 上のレコードとして反映されるまで 5 分以内」。
// ExternalDNS の同期間隔 (既定 1 分) と本 provider の反映待ちを含む。
const defaultWaitBudget = 5 * time.Minute

// pollInterval は DPF への問い合わせ間隔。
//
// 短くしすぎると DPF のレート制限を無駄に消費する。5 分の予算に対して
// 10 秒間隔なら十分な粒度で観測できる。
const pollInterval = 10 * time.Second

// TestSidecarWait は指定した名前が期待する状態になるまで待つ (SC-001)。
//
// ワークフローから 2 回呼ばれる。Ingress の作成後は "present"、削除後は
// "absent" を待つ。
func TestSidecarWait(t *testing.T) {
	target := os.Getenv("DPF_E2E_WAIT_NAME")
	if target == "" {
		t.Skip("DPF_E2E_WAIT_NAME が必要です。サイドカー構成の検証から呼ばれます")
	}

	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	zoneName := os.Getenv("DPF_E2E_ZONE")
	if tokenFile == "" || zoneName == "" {
		t.Fatal("DPF_E2E_TOKEN_FILE と DPF_E2E_ZONE が必要です")
	}

	zone, err := dnsname.Parse(zoneName)
	if err != nil {
		t.Fatalf("DPF_E2E_ZONE を解釈できません: %v", err)
	}
	name, err := dnsname.Parse(target)
	if err != nil {
		t.Fatalf("DPF_E2E_WAIT_NAME を解釈できません: %v", err)
	}

	rrtype := provider.RecordType(envOr("DPF_E2E_WAIT_TYPE", "A"))
	if !provider.IsSupportedRecordType(string(rrtype)) {
		t.Fatalf("DPF_E2E_WAIT_TYPE=%q は対応外の種別です", rrtype)
	}

	state := envOr("DPF_E2E_WAIT_STATE", "present")
	var wantPresent bool
	switch state {
	case "present":
		wantPresent = true
	case "absent":
		wantPresent = false
	default:
		t.Fatalf("DPF_E2E_WAIT_STATE=%q は present か absent でなければなりません", state)
	}

	budget := envDuration(t, "DPF_E2E_WAIT_BUDGET", defaultWaitBudget)

	// 待ち合わせの予算より少し長い文脈を与える。予算切れを
	// 「文脈の打ち切り」ではなく「時間内に反映されなかった」として
	// 報告するため。
	ctx, cancel := context.WithTimeout(t.Context(), budget+time.Minute)
	t.Cleanup(cancel)

	backend, err := dpf.NewClient(ctx, config.DPF{TokenFile: tokenFile}, nil, nil)
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}
	p := provider.New(dnsname.NewScope(zone), backend, slog.New(slog.DiscardHandler))

	deadline := time.Now().Add(budget)
	attempt := 0
	var lastErr error

	for {
		attempt++
		records, err := p.Records(ctx)
		switch {
		case err == nil:
			lastErr = nil
			found := false
			var values []string
			for _, r := range records {
				if r.Name == name && r.Type == rrtype {
					found, values = true, r.Values
					break
				}
			}
			if found == wantPresent {
				elapsed := budget - time.Until(deadline)
				if found {
					t.Logf("%s %s が %s で反映された: %v",
						name, rrtype, elapsed.Round(time.Second), values)
				} else {
					t.Logf("%s %s が %s で削除された", name, rrtype, elapsed.Round(time.Second))
				}
				return
			}

		case errors.Is(err, provider.ErrPermanent):
			// 恒久的な失敗を待ち続けても状況は変わらない。トークンや
			// ゾーン名の誤りをここで打ち切る。
			t.Fatalf("%d 回目の取得が恒久的に失敗: %v", attempt, err)

		default:
			// 一時的な失敗は待つ。クラスタの起動直後は DPF 側も
			// 混み合うことがある。
			lastErr = err
			t.Logf("%d 回目の取得が一時的に失敗 (再試行します): %v", attempt, err)
		}

		if time.Now().After(deadline) {
			break
		}

		select {
		case <-ctx.Done():
			t.Fatalf("待ち合わせが打ち切られた: %v", ctx.Err())
		case <-time.After(pollInterval):
		}
	}

	if lastErr != nil {
		t.Fatalf("%s で %s %s が %s にならなかった (最後の失敗: %v)",
			budget, name, rrtype, state, lastErr)
	}
	t.Fatalf("%s で %s %s が %s にならなかった (SC-001)", budget, name, rrtype, state)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestSidecarForceDelete は印の付いたレコードを直接削除する。
//
// サイドカー構成の検証の後始末に使う。ExternalDNS が Ingress の削除に
// 追随しなかった場合、検証用ゾーンにレコードが残る。残すと以降の実行で
// 件数の表明が崩れ、ゾーンが使えなくなる。
//
// ExternalDNS は所有権を表す TXT レコードも作る。名前の前方一致で拾うため、
// A と TXT の両方が対象になる。
//
//	DPF_E2E_DELETE_PREFIX  削除する名前の接頭辞 (これがある場合のみ実行)
func TestSidecarForceDelete(t *testing.T) {
	prefix := os.Getenv("DPF_E2E_DELETE_PREFIX")
	if prefix == "" {
		t.Skip("DPF_E2E_DELETE_PREFIX が必要です。サイドカー構成の後始末から呼ばれます")
	}

	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	zoneName := os.Getenv("DPF_E2E_ZONE")
	if tokenFile == "" || zoneName == "" {
		t.Fatal("DPF_E2E_TOKEN_FILE と DPF_E2E_ZONE が必要です")
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
	p := provider.New(dnsname.NewScope(zone), backend, slog.New(slog.DiscardHandler))

	records, err := p.Records(ctx)
	if err != nil {
		t.Fatalf("レコード一覧の取得に失敗: %v", err)
	}

	// 接頭辞は正規化して比べる。文字列の見た目で判断すると、
	// 大文字小文字や末尾ドットの違いで取りこぼす。
	want, err := dnsname.Parse(prefix)
	if err != nil {
		t.Fatalf("DPF_E2E_DELETE_PREFIX を解釈できません: %v", err)
	}

	var stale []provider.Record
	for _, r := range records {
		// ExternalDNS の TXT レジストリは "a-<name>" のような接頭辞を
		// 付けた名前も使う。接頭辞を含む名前をまとめて拾う。
		if strings.Contains(r.Name.String(), strings.TrimSuffix(want.String(), ".")) {
			stale = append(stale, r)
		}
	}

	if len(stale) == 0 {
		t.Logf("%s に一致する残留レコードはありません", prefix)
		return
	}

	for _, r := range stale {
		t.Logf("残留レコードを削除します: %s %s", r.Name, r.Type)
	}
	if err := p.ApplyChanges(ctx, provider.ChangeSet{Delete: stale}); err != nil {
		t.Fatalf("残留レコードの削除に失敗: %v", err)
	}
}
