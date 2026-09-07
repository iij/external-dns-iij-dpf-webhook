package dpf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

	tp, err := newTokenProvider(t.Context(), config.DPF{TokenFile: path})
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

	tp, err := newTokenProvider(t.Context(), config.DPF{TokenFile: path})
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

	tp, err := newTokenProvider(t.Context(), config.DPF{TokenFile: path})
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
	_, err := newTokenProvider(t.Context(), config.DPF{})
	if err == nil {
		t.Fatal("環境変数のトークンでプロバイダが作れてしまった")
	}
}

// 未対応のシークレット管理サービスは拒否する (許可リスト方式)。
func TestNewTokenProvider_RejectsUnknownSecretManager(t *testing.T) {
	t.Parallel()

	_, err := newTokenProvider(t.Context(), config.DPF{
		SecretManager: "unknown",
		SecretID:      "id",
	})
	if err == nil {
		t.Fatal("未対応のシークレット管理サービスを受け入れた")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("エラーに指定値が含まれていない: %v", err)
	}
}

// FR-035: 対応する 4 つのシークレット管理サービスが選択肢として存在する。
//
// 実際の接続には各クラウドの資格情報が要るため、ここでは「経路が用意されており、
// 接続前に未対応として弾かれないこと」だけを確かめる。
func TestNewTokenProvider_KnownSecretManagersAreRoutable(t *testing.T) {
	t.Parallel()

	for _, sm := range []string{"vault", "aws", "azure", "gcp"} {
		t.Run(sm, func(t *testing.T) {
			t.Parallel()

			_, err := newTokenProvider(t.Context(), config.DPF{
				SecretManager: sm,
				SecretID:      "some-secret",
			})
			// 接続や資格情報の解決で失敗するのは環境依存であり、ここでは許容する。
			// 許してはならないのは「未対応」として弾かれることだけ。
			if err != nil && strings.Contains(err.Error(), "未対応") {
				t.Errorf("%s が未対応として弾かれた: %v", sm, err)
			}
		})
	}
}
