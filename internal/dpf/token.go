// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	gcpsecretmanager "cloud.google.com/go/secretmanager/apiv1"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/iij/dpf-go/misc/aws"
	"github.com/iij/dpf-go/misc/azure"
	"github.com/iij/dpf-go/misc/gcp"
	"github.com/iij/dpf-go/misc/vault"
	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// newTokenProvider は設定からアクセストークンの供給元を組み立てる。
//
// 供給元はマウントされたファイルと外部シークレット管理サービスの 2 つに限る
// (constitution v1.8.0)。環境変数 DPF_API_TOKEN とコマンドライン引数からは
// 受け取らない。dpf-go の utils.NewClient() は環境変数を既定で参照するため、
// 本サービスはその既定経路を使わず、常に明示的なプロバイダを渡す。
//
// 返される関数は要求のたびに評価される。外部でトークンがローテーションされれば、
// 再起動なしに次の要求から新しい値が使われる (FR-037)。
func newTokenProvider(ctx context.Context, cfg config.DPF) (utils.TokenProvider, error) {
	switch {
	case cfg.TokenFile != "":
		// 呼び出しのたびにファイルを読み直す。Secret のマウント内容が更新されれば
		// 次の要求から反映される。読み取り失敗時にファイルの内容をエラーへ
		// 含めないことは dpf-go 側で保証されている (FR-039)。
		return utils.TokenFromFile(cfg.TokenFile), nil

	case cfg.UsesSecretManager():
		return newSecretManagerProvider(ctx, cfg)

	default:
		// config.Load が先に弾くはずだが、境界の内側でも既定を拒否側に置く (原則 VI)。
		return nil, fmt.Errorf("dpf: アクセストークンの供給元が設定されていません")
	}
}

// newSecretManagerProvider はシークレット管理サービスからトークンを取得する
// プロバイダを組み立てる。
//
// 各サービスへの接続資格情報は、それぞれの標準的な仕組み (環境変数、
// ワークロード ID、インスタンスメタデータなど) から解決される。本サービスは
// それらを設定として受け取らない。
func newSecretManagerProvider(ctx context.Context, cfg config.DPF) (utils.TokenProvider, error) {
	switch cfg.SecretManager {
	case "vault":
		return newVaultProvider(cfg)
	case "aws":
		return newAWSProvider(ctx, cfg)
	case "azure":
		return newAzureProvider(cfg)
	case "gcp":
		return newGCPProvider(ctx, cfg)
	default:
		return nil, fmt.Errorf("dpf: シークレット管理サービス %q は未対応です", cfg.SecretManager)
	}
}

// newVaultProvider は HashiCorp Vault からトークンを取得する。
//
// 接続先と認証は VAULT_ADDR / VAULT_TOKEN などの標準的な環境変数から解決される。
// SecretID はシークレットのパスとして扱う。
func newVaultProvider(cfg config.DPF) (utils.TokenProvider, error) {
	client, err := vaultapi.NewClient(vaultapi.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("dpf: vault クライアントの作成に失敗: %w", err)
	}
	if cfg.SecretEndpoint != "" {
		if setErr := client.SetAddress(cfg.SecretEndpoint); setErr != nil {
			return nil, fmt.Errorf("dpf: vault の接続先の設定に失敗: %w", setErr)
		}
	}

	p, err := vault.NewTokenProvider(client, cfg.SecretID)
	if err != nil {
		return nil, fmt.Errorf("dpf: vault トークンプロバイダの作成に失敗: %w", err)
	}
	return utils.TokenProvider(p), nil
}

// newAWSProvider は AWS Secrets Manager からトークンを取得する。
//
// 資格情報とリージョンは AWS SDK の既定の解決順 (環境変数、共有設定、
// インスタンスプロファイル、IRSA など) に従う。
func newAWSProvider(ctx context.Context, cfg config.DPF) (utils.TokenProvider, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("dpf: aws の設定解決に失敗: %w", err)
	}

	p, err := aws.NewTokenProvider(secretsmanager.NewFromConfig(awsCfg), cfg.SecretID)
	if err != nil {
		return nil, fmt.Errorf("dpf: aws トークンプロバイダの作成に失敗: %w", err)
	}
	return utils.TokenProvider(p), nil
}

// newAzureProvider は Azure Key Vault からトークンを取得する。
//
// Key Vault の URL は他のサービスと異なり環境から導けないため、
// SecretEndpoint で明示的に受け取る。認証は既定の資格情報チェーンによる。
func newAzureProvider(cfg config.DPF) (utils.TokenProvider, error) {
	if cfg.SecretEndpoint == "" {
		return nil, fmt.Errorf(
			"dpf: azure では Key Vault の URL が必要です (--dpf-token-secret-endpoint)")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("dpf: azure の資格情報解決に失敗: %w", err)
	}

	client, err := azsecrets.NewClient(cfg.SecretEndpoint, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("dpf: azure key vault クライアントの作成に失敗: %w", err)
	}

	p, err := azure.NewTokenProvider(client, cfg.SecretID)
	if err != nil {
		return nil, fmt.Errorf("dpf: azure トークンプロバイダの作成に失敗: %w", err)
	}
	return utils.TokenProvider(p), nil
}

// newGCPProvider は Google Secret Manager からトークンを取得する。
//
// 認証はアプリケーションの既定資格情報による。SecretID には
// "projects/PROJECT/secrets/NAME" 形式の名前、または --dpf-token-secret-endpoint に
// プロジェクトを与えたうえでシークレット名のみを指定できる。
func newGCPProvider(ctx context.Context, cfg config.DPF) (utils.TokenProvider, error) {
	client, err := gcpsecretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("dpf: gcp secret manager クライアントの作成に失敗: %w", err)
	}

	var opts []gcp.Option
	if cfg.SecretEndpoint != "" {
		opts = append(opts, gcp.WithProject(cfg.SecretEndpoint))
	}

	p, err := gcp.NewTokenProvider(client, cfg.SecretID, opts...)
	if err != nil {
		return nil, fmt.Errorf("dpf: gcp トークンプロバイダの作成に失敗: %w", err)
	}
	return utils.TokenProvider(p), nil
}
