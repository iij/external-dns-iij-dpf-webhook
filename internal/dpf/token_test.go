// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

func writeToken(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("トークンファイルの書き込みに失敗: %v", err)
	}
	return p
}

// FR-034: トークンをマウントされたファイルから取得できる。
func TestNewTokenProvider_File(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeToken(t, dir, "  token-value\n")

	tp, err := newTokenProvider(config.DPF{TokenFile: path})
	if err != nil {
		t.Fatalf("newTokenProvider = error %v", err)
	}

	got, err := tp(t.Context())
	if err != nil {
		t.Fatalf("トークン取得に失敗: %v", err)
	}
	// 前後の空白は取り除かれる。ファイル末尾の改行がそのまま送られないこと。
	if got != "token-value" {
		t.Errorf("トークン = %q, want %q", got, "token-value")
	}
}

// FR-037: 外部でローテーションされた場合、再起動なしに新しいトークンが使われる。
//
// dpf-go の TokenProvider は要求のたびに評価される。ファイル経路はそのたびに
// 読み直すため、Secret のマウント内容が更新されれば次の要求から反映される。
func TestNewTokenProvider_FileReflectsRotation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeToken(t, dir, "old-token")

	tp, err := newTokenProvider(config.DPF{TokenFile: path})
	if err != nil {
		t.Fatalf("newTokenProvider = error %v", err)
	}

	first, err := tp(t.Context())
	if err != nil {
		t.Fatalf("1 回目の取得に失敗: %v", err)
	}
	if first != "old-token" {
		t.Fatalf("1 回目のトークン = %q, want %q", first, "old-token")
	}

	// 外部でローテーションされた状況を模す。プロバイダは作り直さない。
	writeToken(t, dir, "new-token")

	second, err := tp(t.Context())
	if err != nil {
		t.Fatalf("2 回目の取得に失敗: %v", err)
	}
	if second != "new-token" {
		t.Errorf("ローテーション後のトークン = %q, want %q。再起動なしで反映されねばならない",
			second, "new-token")
	}
}

// FR-039: トークンの取得に失敗しても、エラーにファイルの内容を含めない。
func TestNewTokenProvider_FileErrorDoesNotLeakContent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "missing-token")

	tp, err := newTokenProvider(config.DPF{TokenFile: path})
	if err != nil {
		t.Fatalf("newTokenProvider = error %v", err)
	}

	_, tokErr := tp(t.Context())
	if tokErr == nil {
		t.Fatal("存在しないファイルでトークンを取得できてしまった")
	}

	// 境界を通したエラーは恒久的に分類され、定型文に置き換わる (FR-038/FR-039)。
	// dpf-go は取得失敗を *utils.TokenError に包んで返す。
	classified := Classify(&utils.TokenError{Err: tokErr})
	if !errors.Is(classified, provider.ErrPermanent) {
		t.Errorf("トークン取得失敗が恒久的に分類されていない: %v", classified)
	}
	if strings.Contains(classified.Error(), path) {
		t.Errorf("分類後のエラーにファイルパスが残っている: %v", classified)
	}
}

// FR-036: 環境変数からトークンを受け取らない。
//
// dpf-go の utils.NewClient() は DPF_API_TOKEN を既定で参照するが、
// 本サービスはその経路を使わない (constitution v1.8.0)。
func TestNewTokenProvider_IgnoresEnvironment(t *testing.T) {
	t.Setenv("DPF_API_TOKEN", "token-from-env")

	// 供給元が設定されていなければ、環境変数があってもプロバイダを作れない。
	_, err := newTokenProvider(config.DPF{})
	if err == nil {
		t.Fatal("環境変数のトークンでプロバイダが作れてしまった")
	}
}

// 空白だけのトークンファイルは取得失敗として扱う (spec 007 data-model「トークンファイル」)。
//
// 空の判定は dpf-go が行う (空のトークンを ErrTokenRequired とする)。本サービスは
// 判定を足さないため、その振る舞いをここで固定する。dpf-go の NewClient は設定の誤りを
// 早期に知らせるため構築時に 1 度トークンを取得する。したがって、起動時に空なら
// 起動に失敗し、起動後に空になれば要求ごとに失敗する。
func TestNewClient_BlankTokenFileIsPermanent(t *testing.T) {
	t.Parallel()

	t.Run("起動時に空", func(t *testing.T) {
		t.Parallel()

		path := writeToken(t, t.TempDir(), "  \n")

		_, err := NewClient(t.Context(), config.DPF{TokenFile: path}, nil, nil)
		if err == nil {
			t.Fatal("空白だけのトークンファイルでクライアントを作れてしまった")
		}
		classified := Classify(err)
		if !errors.Is(classified, provider.ErrPermanent) {
			t.Errorf("空のトークンが恒久的な失敗に分類されていない: %v", classified)
		}
		if strings.Contains(classified.Error(), path) {
			t.Errorf("分類後のエラーにファイルパスが残っている: %v", classified)
		}
	})

	t.Run("起動後に空", func(t *testing.T) {
		t.Parallel()

		var requests atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		dir := t.TempDir()
		path := writeToken(t, dir, "valid-token")

		c, err := NewClient(t.Context(), config.DPF{TokenFile: path, Endpoint: srv.URL}, nil, nil)
		if err != nil {
			t.Fatalf("NewClient = error %v", err)
		}

		writeToken(t, dir, "  \n")

		_, err = c.ListZones(t.Context())
		if err == nil {
			t.Fatal("空白だけのトークンファイルで DPF への呼び出しが成功した")
		}
		classified := Classify(err)
		if !errors.Is(classified, provider.ErrPermanent) {
			t.Errorf("空のトークンが恒久的な失敗に分類されていない: %v", classified)
		}
		if strings.Contains(classified.Error(), path) {
			t.Errorf("分類後のエラーにファイルパスが残っている: %v", classified)
		}
		if n := requests.Load(); n != 0 {
			t.Errorf("DPF へ %d 件の要求が送られた。トークンがないまま送ってはならない", n)
		}
	})
}
