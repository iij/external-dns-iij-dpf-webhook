// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"fmt"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// newTokenProvider は設定からアクセストークンの供給元を組み立てる。
//
// 供給元はマウントされたファイルに限る (constitution v3.0.0)。環境変数
// DPF_API_TOKEN とコマンドライン引数からは受け取らない。dpf-go の
// utils.NewClient() は環境変数を既定で参照するため、本サービスはその既定経路を
// 使わず、常に明示的なプロバイダを渡す。
//
// 返される関数は要求のたびに評価される。外部でトークンがローテーションされれば、
// 再起動なしに次の要求から新しい値が使われる (FR-037)。
func newTokenProvider(cfg config.DPF) (utils.TokenProvider, error) {
	if cfg.TokenFile == "" {
		// config.Load が先に弾くはずだが、境界の内側でも既定を拒否側に置く (原則 VI)。
		return nil, fmt.Errorf("dpf: アクセストークンの供給元が設定されていません")
	}

	// 呼び出しのたびにファイルを読み直す。Secret のマウント内容が更新されれば
	// 次の要求から反映される。読み取り失敗時にファイルの内容をエラーへ
	// 含めないことは dpf-go 側で保証されている (FR-039)。
	return utils.TokenFromFile(cfg.TokenFile), nil
}
