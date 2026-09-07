// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// Client は DPF API へのアクセスを提供する。
//
// 本型は internal/provider が宣言する境界 (ports.go) の実装であり、
// dpf-go への依存はここから外へ出ない (原則 II)。
type Client struct {
	api *utils.Client
}

// NewClient は設定から DPF クライアントを組み立てる。
//
// アクセストークンは常に明示的なプロバイダとして渡す。dpf-go の
// utils.NewClient() は環境変数 DPF_API_TOKEN を既定で参照するが、
// 本サービスはその経路を使わない (constitution v1.8.0)。
func NewClient(ctx context.Context, cfg config.DPF) (*Client, error) {
	tp, err := newTokenProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	opts := []utils.ClientOption{utils.WithTokenProvider(tp)}

	if cfg.Endpoint != "" {
		opts = append(opts, utils.WithEndpoint(cfg.Endpoint))
	}

	// シークレット管理サービスは呼び出しごとに外部へ問い合わせる。要求のたびに
	// 取得すると相手側への負荷とレイテンシが無視できないため、短時間だけ保持する。
	//
	// ファイル経路にはキャッシュを設けない。読み取りは十分に安価であり、
	// ローテーションの反映を遅らせる理由がないため (FR-037)。
	if cfg.UsesSecretManager() {
		opts = append(opts, utils.WithTokenTTL(secretManagerTokenTTL))
	}

	api, err := utils.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("dpf: クライアントの作成に失敗: %w", err)
	}

	return &Client{api: api}, nil
}

// secretManagerTokenTTL はシークレット管理サービスから取得したトークンを保持する時間。
//
// ローテーションの反映が最大でこの時間だけ遅れる。長くするほど外部への
// 問い合わせは減るが、失効したトークンを使い続ける窓が広がる。
const secretManagerTokenTTL = 30 * time.Second
