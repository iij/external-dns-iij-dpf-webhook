// SPDX-License-Identifier: Apache-2.0

package webhook

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// Handler は ExternalDNS webhook provider API を提供する。
type Handler struct {
	provider *provider.Provider
}

// NewHandler は provider を包む HTTP ハンドラを返す。
//
// 経路は上流仕様が定める 4 つに限る。独自のエンドポイントを足さないこと (原則 I)。
func NewHandler(p *provider.Provider) http.Handler {
	h := &Handler{provider: p}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.getRoot)
	mux.HandleFunc("GET /records", h.getRecords)
	mux.HandleFunc("POST /records", h.postRecords)
	mux.HandleFunc("POST /adjustendpoints", h.postAdjustEndpoints)

	return Negotiate(mux)
}

// getRoot は管理対象ドメインを返す (GET /)。
//
// 範囲が空なら空の filters を返す。全ドメインを表す応答にしてはならない (FR-002)。
func (h *Handler) getRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, filtersResponse{Filters: h.provider.Filters()})
}

// logRequestBody は要求本文を debug で記録し、読み直せる形にして返す。
//
// **ドメインの型は上流の全フィールドを持たない。** `updateOld`、`labels`、
// `providerSpecific`、`setIdentifier` は provider.Record に写されないため、
// 変換後のログでは見えない。差分が振動したとき、**ExternalDNS が「現在こうだ」と
// 見なした値 (updateOld) と「こうしたい」(updateNew) を並べれば、どのフィールドを
// 差分と判断したのかが ExternalDNS の計算結果として直接読める。**
//
// ExternalDNS 側は差分の判断をログに出さない (debug にしても出ない)。
// したがって受け取った本文をそのまま残すほかない。
//
// debug でないときは本文を読まずにそのまま返す。1,000 件規模の要求を
// 常時メモリへ写すことはしない。
func (h *Handler) logRequestBody(r *http.Request) io.Reader {
	logger := h.provider.Logger()
	if !logger.Enabled(r.Context(), slog.LevelDebug) {
		return r.Body
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, requestLogLimit+1))
	if err != nil {
		logger.DebugContext(r.Context(), "要求本文を読めませんでした", "error", err)
		return r.Body
	}

	shown, truncated := raw, false
	if len(shown) > requestLogLimit {
		shown, truncated = shown[:requestLogLimit], true
	}
	logger.DebugContext(r.Context(), "要求本文",
		"path", r.URL.Path,
		"bytes", len(raw),
		"truncated", truncated,
		"body", string(shown),
	)

	// 読み切った分と、上限を超えて残っている分を繋ぎ直す。
	return io.MultiReader(bytes.NewReader(raw), r.Body)
}

// requestLogLimit は debug に出す要求本文の上限。
//
// 原因は要求の構造に現れる。全文を出すと、規模の大きいゾーンでログが埋まる。
const requestLogLimit = 16 << 10

// getRecords は管理対象のレコード一覧を返す (GET /records)。
func (h *Handler) getRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.provider.Records(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, toEndpoints(records))
}

// postRecords は変更セットを適用する (POST /records)。
//
// 成功時は 204 No Content を返す。上流仕様がこの値を定めており、200 ではない。
// 反映が完了する前に成功を返さない (FR-011)。
func (h *Handler) postRecords(w http.ResponseWriter, r *http.Request) {
	body := h.logRequestBody(r)

	var c changes
	if err := json.NewDecoder(body).Decode(&c); err != nil {
		// 解釈できない要求は再試行しても同じように失敗する。
		WriteError(w, fmt.Errorf("%w: 要求を解釈できません: %w", provider.ErrPermanent, err))
		return
	}

	cs, err := toChangeSet(c)
	if err != nil {
		WriteError(w, err)
		return
	}

	if err := h.provider.ApplyChanges(r.Context(), cs); err != nil {
		WriteError(w, err)
		return
	}

	// 204 には本文を伴わせない。Content-Type は Negotiate が設定済みだが、
	// 本文がないためどちらでも解釈は変わらない。
	w.WriteHeader(http.StatusNoContent)
}

// postAdjustEndpoints はレコードを DPF に保存される形へ整えて返す
// (POST /adjustendpoints)。
//
// ExternalDNS が期待する値と DPF に実際に保存される値を一致させるための操作で
// ある。食い違うと、ExternalDNS は毎回差分を検出して同じ変更を適用し続ける
// (SC-007)。
//
// DPF には触れない。表現を整えるだけであり、外部の状態を必要としない。
func (h *Handler) postAdjustEndpoints(w http.ResponseWriter, r *http.Request) {
	var in []endpoint
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		WriteError(w, fmt.Errorf("%w: 要求を解釈できません: %w", provider.ErrPermanent, err))
		return
	}

	records, err := toRecords(in)
	if err != nil {
		WriteError(w, err)
		return
	}

	writeJSON(w, adjustEndpoints(in, provider.Adjust(records)))
}

// writeJSON は v を JSON として書く。
//
// 符号化に失敗した時点で応答本文は書き始められているため、状態コードは
// 変更できない。失敗はログに残す責務を呼び出し側に委ねず、ここでは
// 応答を打ち切るに留める。
func writeJSON(w http.ResponseWriter, v any) {
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		// ヘッダ送出後のため状態コードは変えられない。接続を壊して
		// 不完全な本文を「成功」として受け取らせない。
		panic(http.ErrAbortHandler)
	}
}
