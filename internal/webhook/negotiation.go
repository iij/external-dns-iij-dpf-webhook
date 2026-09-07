// Package webhook は ExternalDNS webhook provider API の HTTP 層を担う。
//
// 本パッケージの範囲は契約の実装に限る。ドメインロジックは internal/provider に、
// DPF へのアクセスは internal/dpf にあり、いずれもここには現れない。
//
// 独自のエンドポイントやフィールドを追加しない (原則 I)。DPF 固有の挙動が必要な
// 場合は設定として表現し、契約は変更しない。
package webhook

import (
	"mime"
	"net/http"
	"strings"
)

// MediaType は ExternalDNS webhook provider API のメディアタイプ。
//
// 上流仕様 (api/webhook.yaml v0.22.0) が定める値であり、version パラメータを含む。
// 上流がこの version を変更した場合、自動的に追随しない。constitution を改訂した
// うえで対応方針を決める。
const MediaType = "application/external.dns.webhook+json;version=1"

// mediaTypeBase は version パラメータを除いた部分。
const mediaTypeBase = "application/external.dns.webhook+json"

// mediaTypeVersion は本サービスが対応する version。
const mediaTypeVersion = "1"

// Negotiate は Accept ヘッダを検査し、応答に Content-Type を設定するミドルウェア。
//
// ネゴシエートできない要求は 406 で拒否する。受け入れる場合は下位のハンドラを呼ぶ前に
// Content-Type を設定する。ハンドラが本文を書き始めた後ではヘッダを変更できないため。
func Negotiate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptable(r.Header.Get("Accept")) {
			http.Error(w, "unsupported media type", http.StatusNotAcceptable)
			return
		}

		w.Header().Set("Content-Type", MediaType)
		next.ServeHTTP(w, r)
	})
}

// acceptable は Accept ヘッダの値を本サービスが満たせるかを判定する。
//
// 空白や引用符の有無、パラメータの順序、大文字小文字は送信側の実装に依存する。
// これらの差異を理由に拒否すると、上流の実装が変わっただけで動かなくなる。
// 判定するのは「型が一致すること」と「version が対応範囲にあること」の 2 点に絞る。
func acceptable(accept string) bool {
	// 未指定は「何でもよい」と解釈する。
	if strings.TrimSpace(accept) == "" {
		return true
	}

	for _, entry := range strings.Split(accept, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		typ, params, err := mime.ParseMediaType(entry)
		if err != nil {
			continue
		}

		if typ == "*/*" || typ == "application/*" {
			return true
		}
		if typ != mediaTypeBase {
			continue
		}

		// version の指定がなければ、対応する版で応答してよい。
		v, ok := params["version"]
		if !ok || v == mediaTypeVersion {
			return true
		}
	}

	return false
}
