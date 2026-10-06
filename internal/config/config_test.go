// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// tokenFile はトークンを収めた一時ファイルを作り、そのパスを返す。
func tokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("トークンファイルの作成に失敗: %v", err)
	}
	return p
}

// baseArgs は必須設定を満たす最小の引数を返す。
func baseArgs(t *testing.T) []string {
	t.Helper()
	return []string{"--dpf-token-file", tokenFile(t, "dummy-token")}
}

// FR-017: 必須設定が欠けた状態で起動してはならない。
//
// 供給元はファイルだけである (spec 007 FR-003 / constitution v3.0.0)。
// メッセージは指定すべきフラグを示し、廃止した経路を案内しない。
func TestLoad_RequiresTokenSource(t *testing.T) {
	t.Parallel()

	_, err := config.Load(nil)
	if err == nil {
		t.Fatal("トークン供給元なしで Load が成功した。起動を許してはならない")
	}
	if !errors.Is(err, config.ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
	if !strings.Contains(err.Error(), "--dpf-token-file") {
		t.Errorf("エラーが --dpf-token-file を示していない: %v", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "secret") {
		t.Errorf("エラーが廃止したシークレット管理サービスの経路を案内している: %v", err)
	}
}

// spec 007 FR-004 / constitution v3.0.0: シークレット管理サービス用のフラグは
// 廃止した。ファイルと併せて指定されても黙って無視せず、起動を中止する。
// どのフラグが受け付けられなかったかをメッセージで示す。
func TestLoad_RejectsRemovedSecretManagerFlags(t *testing.T) {
	t.Parallel()

	for _, arg := range []string{
		"--dpf-token-secret-manager=aws",
		"--dpf-token-secret-id=x",
		"--dpf-token-secret-endpoint=https://example.invalid/",
	} {
		name := strings.TrimPrefix(strings.SplitN(arg, "=", 2)[0], "--")
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(append(baseArgs(t), arg))
			if err == nil {
				t.Fatalf("%s が受け付けられた。廃止したフラグは起動を中止させねばならない", arg)
			}
			if !errors.Is(err, config.ErrInvalid) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("エラーに受け付けなかったフラグ名 %q が含まれない: %v", name, err)
			}
		})
	}
}

// spec 007 FR-004: ファイルを指定せず、廃止したフラグだけで起動しようとしても
// 受け付けない。供給元の不足ではなく、廃止したフラグとして報告する。
func TestLoad_RejectsRemovedFlagsWithoutTokenFile(t *testing.T) {
	t.Parallel()

	_, err := config.Load([]string{
		"--dpf-token-secret-manager=aws",
		"--dpf-token-secret-id=x",
	})
	if err == nil {
		t.Fatal("廃止したフラグだけで Load が成功した")
	}
	if !errors.Is(err, config.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
	if !strings.Contains(err.Error(), "dpf-token-secret-manager") {
		t.Errorf("エラーに受け付けなかったフラグ名が含まれない: %v", err)
	}
}

// constitution v1.8.0: トークンは環境変数から受け取ってはならない (MUST NOT)。
// dpf-go の既定経路 DPF_API_TOKEN を塞いでいることを確かめる。
func TestLoad_IgnoresTokenEnvironmentVariable(t *testing.T) {
	// 環境変数を触るため t.Parallel() は使わない。
	t.Setenv("DPF_API_TOKEN", "token-from-env")

	_, err := config.Load(nil)
	if err == nil {
		t.Fatal("環境変数のトークンで起動できてしまった。環境変数経路は塞がねばならない")
	}
	if !errors.Is(err, config.ErrMissingRequired) {
		t.Errorf("err = %v, want ErrMissingRequired", err)
	}
}

// FR-034: トークンをマウントされたファイルとして与えられる。
func TestLoad_AcceptsTokenFile(t *testing.T) {
	t.Parallel()

	path := tokenFile(t, "dummy-token")
	cfg, err := config.Load([]string{"--dpf-token-file", path})
	if err != nil {
		t.Fatalf("Load = error %v, want success", err)
	}
	if cfg.DPF.TokenFile != path {
		t.Errorf("TokenFile = %q, want %q", cfg.DPF.TokenFile, path)
	}
}

// FR-035: トークンを外部のシークレット管理サービスから取得させられる。
func TestLoad_AcceptsSecretManager(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load([]string{
		"--dpf-token-secret-manager", "aws",
		"--dpf-token-secret-id", "prod/dpf/token",
	})
	if err != nil {
		t.Fatalf("Load = error %v, want success", err)
	}
	if cfg.DPF.SecretManager != "aws" {
		t.Errorf("SecretManager = %q, want %q", cfg.DPF.SecretManager, "aws")
	}
	if cfg.DPF.SecretID != "prod/dpf/token" {
		t.Errorf("SecretID = %q, want %q", cfg.DPF.SecretID, "prod/dpf/token")
	}
}

// 供給元は 1 つに定める。両方指定はどちらが使われるか曖昧になるため拒否する。
func TestLoad_RejectsBothTokenSources(t *testing.T) {
	t.Parallel()

	_, err := config.Load([]string{
		"--dpf-token-file", tokenFile(t, "dummy-token"),
		"--dpf-token-secret-manager", "aws",
		"--dpf-token-secret-id", "prod/dpf/token",
	})
	if err == nil {
		t.Fatal("2 つのトークン供給元を同時に指定して成功した")
	}
}

// 対応していないシークレット管理サービスは拒否する (許可リスト方式、原則 VI)。
func TestLoad_RejectsUnknownSecretManager(t *testing.T) {
	t.Parallel()

	_, err := config.Load([]string{
		"--dpf-token-secret-manager", "unknown-vault",
		"--dpf-token-secret-id", "id",
	})
	if err == nil {
		t.Fatal("未対応のシークレット管理サービスを受け入れた")
	}
}

// シークレット管理サービスを指定したら、対象の識別子も要る。
func TestLoad_SecretManagerRequiresSecretID(t *testing.T) {
	t.Parallel()

	_, err := config.Load([]string{"--dpf-token-secret-manager", "aws"})
	if err == nil {
		t.Fatal("シークレット識別子なしで受け入れた")
	}
}

// FR-002: domain filter 未設定は「範囲なし」。全ドメイン管理に読み替えない。
func TestLoad_EmptyDomainFilterMeansNoScope(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(baseArgs(t))
	if err != nil {
		t.Fatalf("Load = error %v, want success", err)
	}
	if !cfg.Scope.IsEmpty() {
		t.Fatal("domain filter 未設定で範囲が空でない。default-deny が破れている")
	}
	for _, s := range []string{"example.jp", "a.example.jp"} {
		if cfg.Scope.Contains(dnsname.MustParse(s)) {
			t.Errorf("未設定の範囲が %q を含むと判定された", s)
		}
	}
}

// domain filter は複数指定でき、受け取った時点で正規化される。
func TestLoad_DomainFilterIsCanonicalized(t *testing.T) {
	t.Parallel()

	args := append(baseArgs(t),
		"--domain-filter", "EXAMPLE.JP",
		"--domain-filter", "example.com.",
	)
	cfg, err := config.Load(args)
	if err != nil {
		t.Fatalf("Load = error %v, want success", err)
	}

	got := make([]string, 0, 2)
	for _, d := range cfg.Scope.Domains() {
		got = append(got, d.String())
	}
	want := []string{"example.jp.", "example.com."}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Scope.Domains() = %v, want %v", got, want)
	}
}

// FR-018: 解釈に失敗した設定値で、既定値にフォールバックして起動を続行しない。
func TestLoad_RejectsInvalidDomainFilter(t *testing.T) {
	t.Parallel()

	args := append(baseArgs(t), "--domain-filter", "not..a..name")
	if _, err := config.Load(args); err == nil {
		t.Fatal("妥当でない domain filter を受け入れた。既定値へのフォールバックは禁止")
	}
}

func TestLoad_RejectsInvalidLogLevel(t *testing.T) {
	t.Parallel()

	args := append(baseArgs(t), "--log-level", "verbose")
	if _, err := config.Load(args); err == nil {
		t.Fatal("未知のログレベルを受け入れた")
	}
}

// 原則 VI: provider エンドポイントは既定でループバックのみに待ち受ける。
func TestLoad_ProviderBindsLoopbackByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(baseArgs(t))
	if err != nil {
		t.Fatalf("Load = error %v", err)
	}
	if cfg.Server.ProviderAddr != "127.0.0.1:8888" {
		t.Errorf("ProviderAddr = %q, want %q", cfg.Server.ProviderAddr, "127.0.0.1:8888")
	}
	if cfg.Server.ExposedAddr != ":8080" {
		t.Errorf("ExposedAddr = %q, want %q", cfg.Server.ExposedAddr, ":8080")
	}
}

// 原則 VI: OTLP は送出先が設定された場合にのみ有効になる。
func TestLoad_OTLPDisabledByDefault(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load(baseArgs(t))
	if err != nil {
		t.Fatalf("Load = error %v", err)
	}
	if cfg.Telemetry.OTLPEnabled() {
		t.Error("OTLP が既定で有効になっている。送出は opt-in でなければならない")
	}
}

func TestLoad_OTLPEnabledWhenEndpointSet(t *testing.T) {
	t.Parallel()

	args := append(baseArgs(t), "--otlp-endpoint", "collector.example.jp:4317")
	cfg, err := config.Load(args)
	if err != nil {
		t.Fatalf("Load = error %v", err)
	}
	if !cfg.Telemetry.OTLPEnabled() {
		t.Error("送出先を設定しても OTLP が有効にならない")
	}
	if cfg.Telemetry.OTLPInsecure {
		t.Error("OTLP の TLS 検証が既定で無効になっている")
	}
}

// --help は使い方を返し、エラーにしない。
//
// フラグを持つプログラムが使い方を示せないと、利用者は設定項目を
// ソースを読まずに知る手段がない。
func TestLoad_HelpReturnsUsage(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"-h", "--help"} {
		_, err := config.Load([]string{flag})
		if !errors.Is(err, config.ErrHelpRequested) {
			t.Errorf("%s = %v, want ErrHelpRequested", flag, err)
		}
	}
}

// 使い方の文面に主要な設定項目が現れる。
func TestUsage_ListsKeySettings(t *testing.T) {
	t.Parallel()

	usage := config.Usage()

	// flag.PrintDefaults はダッシュ 1 つで出力する。両形式が受け付けられる。
	for _, want := range []string{
		"-domain-filter",
		"-dpf-token-file",
		"-provider-addr",
		"-exposed-addr",
		"-otlp-endpoint",
		"-log-level",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("使い方に %q が現れない:\n%s", want, usage)
		}
	}
}

// 使い方にトークンを直接渡す項目が現れない (constitution v1.8.0)。
func TestUsage_HasNoInlineTokenOption(t *testing.T) {
	t.Parallel()

	usage := config.Usage()
	for _, forbidden := range []string{"-dpf-token ", "-dpf-api-token"} {
		if strings.Contains(usage, forbidden) {
			t.Errorf("使い方に %q が現れた。トークンを引数で渡す経路は作らない", forbidden)
		}
	}
}

// 使い方にシークレット管理サービスの項目や案内が現れない
// (spec 007 SC-001 / constitution v3.0.0)。
func TestUsage_HasNoSecretManagerOption(t *testing.T) {
	t.Parallel()

	usage := config.Usage()
	if strings.Contains(strings.ToLower(usage), "secret") {
		t.Errorf("使い方にシークレット管理サービスの項目が現れた:\n%s", usage)
	}
}

// 設定値そのものにトークンを書かせる経路を用意しない (constitution v1.8.0)。
func TestLoad_HasNoInlineTokenFlag(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"--dpf-token", "--token", "--dpf-api-token"} {
		if _, err := config.Load([]string{flag, "secret"}); err == nil {
			t.Errorf("%s が受け付けられた。トークンを引数で渡す経路を作ってはならない", flag)
		}
	}
}
