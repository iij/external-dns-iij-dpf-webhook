// Package config は起動時の設定を読み込み、検証する。
//
// 本パッケージの設計方針は原則 VI (Default-Deny) に従う。
//   - 設定を与えなかった場合の既定動作は「何もしない・何も許さない」
//   - 未設定を「すべて許可」と解釈しない
//   - 解釈に失敗した場合は既定値へフォールバックせず、起動を中止する
//
// トークンの取得経路は、マウントされたファイルと外部シークレット管理サービスの
// 2 つに限る (constitution v1.8.0)。環境変数とコマンドライン引数から
// トークンを受け取る経路は提供しない。環境変数は Pod の情報表示やプロセスの環境から
// 読み取れ、ファイルおよびシークレット管理サービスと同じ保護水準を満たさないため。
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// ErrMissingRequired は必須設定が欠けていることを表す。
var ErrMissingRequired = errors.New("config: required setting is missing")

// ErrInvalid は設定値を解釈できないことを表す。
var ErrInvalid = errors.New("config: invalid setting")

// supportedSecretManagers は対応するシークレット管理サービスの許可リスト。
//
// dpf-go が別モジュールとして提供する範囲を上限とする。許可リスト方式にするのは、
// 未知の値を黙って受け入れて実行時に失敗させないため (原則 VI)。
var supportedSecretManagers = []string{"vault", "aws", "azure", "gcp"}

// Config は本サービスの設定全体を表す。
type Config struct {
	// Scope は管理対象範囲。空は「範囲なし」を意味する (FR-002)。
	Scope dnsname.Scope

	DPF       DPF
	Server    Server
	Telemetry Telemetry
	LogLevel  slog.Level
}

// DPF は DPF API への接続に関する設定。
type DPF struct {
	// Endpoint は DPF API のエンドポイント。空なら dpf-go の既定値を使う。
	Endpoint string

	// TokenFile はトークンを収めたファイルのパス。
	// 要求のたびに読み直されるため、外部でローテーションされれば再起動なしに反映される。
	TokenFile string

	// SecretManager は利用するシークレット管理サービス。supportedSecretManagers のいずれか。
	SecretManager string

	// SecretID はシークレット管理サービス上の識別子。
	SecretID string
}

// UsesSecretManager はトークンをシークレット管理サービスから取得するかを報告する。
func (d DPF) UsesSecretManager() bool { return d.SecretManager != "" }

// Server は待ち受けアドレスの設定。
type Server struct {
	// ProviderAddr は webhook provider エンドポイントの待ち受けアドレス。
	//
	// 既定はループバックのみ。ExternalDNS とは同一 Pod 内のサイドカーとして通信するため、
	// Pod 外への公開は既定では不要である (原則 VI)。
	ProviderAddr string

	// ExposedAddr は healthz と metrics の待ち受けアドレス。
	// kubelet からの probe と Prometheus のスクレイプを受けるため、Pod 外から到達できる。
	ExposedAddr string
}

// Telemetry はテレメトリの設定。
type Telemetry struct {
	// OTLPEndpoint は OTLP の送出先。空なら送出しない (原則 VI)。
	OTLPEndpoint string

	// OTLPProtocol は "grpc" または "http"。
	OTLPProtocol string

	// OTLPInsecure は TLS 検証を無効にする。既定は false。
	// 有効にした場合、警告をログに出す責任は利用側にある。
	OTLPInsecure bool
}

// OTLPEnabled は OTLP による送出が有効かを報告する。
// 送出先が設定された場合にのみ有効になる。
func (t Telemetry) OTLPEnabled() bool { return t.OTLPEndpoint != "" }

// domainFilterFlag は --domain-filter の複数指定を受ける。
// 受け取った時点で正規化し、解釈できない値はその場で失敗させる。
type domainFilterFlag []dnsname.Name

func (f *domainFilterFlag) String() string { return "" }

func (f *domainFilterFlag) Set(v string) error {
	n, err := dnsname.Parse(v)
	if err != nil {
		return fmt.Errorf("%w: --domain-filter %q: %w", ErrInvalid, v, err)
	}
	*f = append(*f, n)
	return nil
}

// Load は args を解釈して設定を返す。
//
// 必須設定が欠けている場合、および値を解釈できない場合はエラーを返す。
// 呼び出し側はこの場合に起動を中止すること。既定値で続行してはならない (FR-018)。
func Load(args []string) (Config, error) {
	fs := flag.NewFlagSet("webhook", flag.ContinueOnError)
	// 使い方の出力は呼び出し側に委ねる。ここでは黙って失敗させる。
	fs.SetOutput(io.Discard)

	var (
		domains       domainFilterFlag
		dpfEndpoint   = fs.String("dpf-endpoint", "", "DPF API のエンドポイント (未指定なら既定値)")
		tokenFile     = fs.String("dpf-token-file", "", "DPF アクセストークンを収めたファイルのパス")
		secretManager = fs.String("dpf-token-secret-manager", "", "トークンを取得するシークレット管理サービス (vault|aws|azure|gcp)")
		secretID      = fs.String("dpf-token-secret-id", "", "シークレット管理サービス上の識別子")
		providerAddr  = fs.String("provider-addr", "127.0.0.1:8888", "webhook provider エンドポイントの待ち受けアドレス")
		exposedAddr   = fs.String("exposed-addr", ":8080", "healthz と metrics の待ち受けアドレス")
		otlpEndpoint  = fs.String("otlp-endpoint", "", "OTLP の送出先 (未指定なら送出しない)")
		otlpProtocol  = fs.String("otlp-protocol", "grpc", "OTLP のプロトコル (grpc|http)")
		otlpInsecure  = fs.Bool("otlp-insecure", false, "OTLP 送出先への TLS 検証を無効にする")
		logLevel      = fs.String("log-level", "info", "ログレベル (debug|info|warn|error)")
	)
	fs.Var(&domains, "domain-filter", "管理対象ドメイン (複数指定可、未指定なら管理対象なし)")

	// 意図的に定義しないフラグ:
	//   --dpf-token         トークンを引数で渡す経路は作らない (constitution v1.8.0)
	//   環境変数 DPF_API_TOKEN も参照しない。dpf-go の既定経路を使わないのはそのため。
	// flag はこれらを未定義として拒否するため、指定すると起動に失敗する。

	if err := fs.Parse(args); err != nil {
		return Config{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}

	level, err := parseLogLevel(*logLevel)
	if err != nil {
		return Config{}, err
	}

	dpf := DPF{
		Endpoint:      *dpfEndpoint,
		TokenFile:     *tokenFile,
		SecretManager: *secretManager,
		SecretID:      *secretID,
	}
	if err := validateTokenSource(dpf); err != nil {
		return Config{}, err
	}

	if err := validateOTLPProtocol(*otlpProtocol); err != nil {
		return Config{}, err
	}

	return Config{
		Scope: dnsname.NewScope(domains...),
		DPF:   dpf,
		Server: Server{
			ProviderAddr: *providerAddr,
			ExposedAddr:  *exposedAddr,
		},
		Telemetry: Telemetry{
			OTLPEndpoint: *otlpEndpoint,
			OTLPProtocol: *otlpProtocol,
			OTLPInsecure: *otlpInsecure,
		},
		LogLevel: level,
	}, nil
}

// validateTokenSource はトークン供給元がちょうど 1 つ指定されていることを確かめる。
//
// 0 個なら起動できない (FR-017)。2 個ならどちらが使われるか曖昧になるため拒否する。
func validateTokenSource(d DPF) error {
	hasFile := d.TokenFile != ""
	hasSM := d.SecretManager != "" || d.SecretID != ""

	switch {
	case !hasFile && !hasSM:
		return fmt.Errorf("%w: トークンの供給元を指定してください "+
			"(--dpf-token-file、または --dpf-token-secret-manager と --dpf-token-secret-id)",
			ErrMissingRequired)

	case hasFile && hasSM:
		return fmt.Errorf("%w: トークンの供給元は 1 つだけ指定してください "+
			"(--dpf-token-file と --dpf-token-secret-manager の併用は不可)", ErrInvalid)

	case hasSM:
		if d.SecretManager == "" {
			return fmt.Errorf("%w: --dpf-token-secret-manager を指定してください", ErrMissingRequired)
		}
		if d.SecretID == "" {
			return fmt.Errorf("%w: --dpf-token-secret-id を指定してください", ErrMissingRequired)
		}
		if !slices.Contains(supportedSecretManagers, d.SecretManager) {
			return fmt.Errorf("%w: --dpf-token-secret-manager %q は未対応です (対応: %v)",
				ErrInvalid, d.SecretManager, supportedSecretManagers)
		}
	}

	return nil
}

func validateOTLPProtocol(p string) error {
	if p != "grpc" && p != "http" {
		return fmt.Errorf("%w: --otlp-protocol %q は grpc または http を指定してください", ErrInvalid, p)
	}
	return nil
}

func parseLogLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%w: --log-level %q は debug|info|warn|error を指定してください", ErrInvalid, s)
	}
}
