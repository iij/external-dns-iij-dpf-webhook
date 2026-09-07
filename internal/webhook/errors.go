// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"errors"
	"net/http"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// WriteError はエラーの分類に応じた状態コードで応答する。
//
// contracts/webhook-api.md が定める対応:
//
//	一時的な障害 → 5xx  ExternalDNS は再試行する
//	恒久的な障害 → 4xx  ExternalDNS は再試行しない
//
// 分類のないエラーは 5xx で返す。恒久的として返すと ExternalDNS が再試行を諦め、
// 実際には復旧しうる障害で DNS の更新が止まる。判断がつかないときは再試行の
// 余地を残す方が害が小さい。
//
// 応答本文にエラーの詳細を載せない。詳細はログへ出す。応答に載せると、
// DNS 構成や内部状態が呼び出し側へ漏れる経路になる。
func WriteError(w http.ResponseWriter, err error) {
	http.Error(w, http.StatusText(statusFor(err)), statusFor(err))
}

// statusFor はエラーの分類から状態コードを決める。
func statusFor(err error) int {
	switch {
	case err == nil:
		return http.StatusOK

	// 恒久的な分類を先に見る。両方に一致することは想定しないが、
	// 誤って両方が付いた場合は「再試行させない」側を採らない。
	case errors.Is(err, provider.ErrTemporary):
		return http.StatusInternalServerError

	case errors.Is(err, provider.ErrPermanent),
		errors.Is(err, provider.ErrUnsupportedType),
		errors.Is(err, provider.ErrZoneNotFound):
		return http.StatusBadRequest

	default:
		return http.StatusInternalServerError
	}
}
