// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// TestTokenRotation はトークンのローテーションが再起動なしに反映されることを
// 確かめる (quickstart §8、SC-009、FR-037/FR-038/FR-039)。
//
// トークンには有効期限があり、運用中に必ず差し替えが起きる。再起動が必要な
// 実装では、差し替えのたびに DNS の更新が止まる。
//
// 検証は本物のトークンの写しに対して行う。元のファイルは書き換えない。
// 同じ実行の他のテストがそれを使っているうえ、CI では後始末で消される。
func TestTokenRotation(t *testing.T) {
	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	zoneName := os.Getenv("DPF_E2E_ZONE")
	if tokenFile == "" || zoneName == "" {
		t.Skip("DPF_E2E_TOKEN_FILE と DPF_E2E_ZONE が必要です。検証用ゾーンでのみ実行してください")
	}

	zone, err := dnsname.Parse(zoneName)
	if err != nil {
		t.Fatalf("DPF_E2E_ZONE を解釈できません: %v", err)
	}

	valid, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("トークンファイルを読めません: %v", err)
	}
	token := strings.TrimSpace(string(valid))

	// 写しを作る。パーミッションは所有者のみ。
	rotating := filepath.Join(t.TempDir(), "token")
	write := func(t *testing.T, content string) {
		t.Helper()
		if writeErr := os.WriteFile(rotating, []byte(content), 0o600); writeErr != nil {
			t.Fatalf("トークンファイルの書き込みに失敗: %v", writeErr)
		}
	}
	write(t, token)

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	backend, err := dpf.NewClient(ctx, config.DPF{TokenFile: rotating}, nil, nil)
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}
	p := provider.New(dnsname.NewScope(zone), backend, slog.New(slog.DiscardHandler))

	// DNS を変更しない。読み取りだけでトークンの有効性は判定できる。
	read := func(t *testing.T) error {
		t.Helper()
		_, err := p.Records(t.Context())
		return err
	}

	t.Run("有効なトークンで読み取れる", func(t *testing.T) {
		if err := read(t); err != nil {
			t.Fatalf("読み取りに失敗: %v", err)
		}
	})

	// **クライアントを作り直さない。** 同じインスタンスのまま、ファイルの
	// 内容だけを差し替える。これが「再起動なしで反映される」の意味である。
	t.Run("無効な値へ差し替えると次の要求から失敗する", func(t *testing.T) {
		write(t, "invalid-token-for-rotation-check")

		err := read(t)
		if err == nil {
			t.Fatal("無効なトークンで読み取れてしまった。ファイルが読み直されていない (FR-037)")
		}

		// 無効なトークンは再試行しても通らない。恒久的な失敗であること。
		if !errors.Is(err, provider.ErrPermanent) {
			t.Errorf("分類 = %v, want ErrPermanent。再試行を繰り返す (FR-038)", err)
		}
		assertNoToken(t, err, token)
	})

	t.Run("元に戻すと次の要求から成功する", func(t *testing.T) {
		write(t, token)

		if err := read(t); err != nil {
			t.Fatalf("差し戻し後の読み取りに失敗: %v。ローテーションが反映されていない (FR-037)", err)
		}
	})

	t.Run("ファイルを消すと恒久的な失敗になる", func(t *testing.T) {
		if err := os.Remove(rotating); err != nil {
			t.Fatalf("トークンファイルの削除に失敗: %v", err)
		}

		err := read(t)
		if err == nil {
			t.Fatal("トークンファイルがないのに読み取れてしまった")
		}

		// 取得できない状態は同じ要求を繰り返しても解消しない。一時的として
		// 返すと ExternalDNS が再試行を続け、無駄な負荷になる (FR-038)。
		if !errors.Is(err, provider.ErrPermanent) {
			t.Errorf("分類 = %v, want ErrPermanent (FR-038)", err)
		}
		assertNoToken(t, err, token)
	})
}

// assertNoToken はエラーメッセージにトークンが現れないことを確かめる (FR-039)。
//
// **トークン自体を出力しない。** 表明が失敗しても値を漏らさないよう、
// メッセージには「含まれていた」ことだけを書く。
func assertNoToken(t *testing.T, err error, token string) {
	t.Helper()

	if token == "" {
		return
	}
	if strings.Contains(err.Error(), token) {
		t.Error("エラーメッセージにトークンが現れる (FR-039)")
	}
}
