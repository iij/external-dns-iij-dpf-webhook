package webhook

import (
	"encoding/json"
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

	// POST /records と POST /adjustendpoints は US2・US3 で追加する。

	return Negotiate(mux)
}

// getRoot は管理対象ドメインを返す (GET /)。
//
// 範囲が空なら空の filters を返す。全ドメインを表す応答にしてはならない (FR-002)。
func (h *Handler) getRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, filtersResponse{Filters: h.provider.Filters()})
}

// getRecords は管理対象のレコード一覧を返す (GET /records)。
func (h *Handler) getRecords(w http.ResponseWriter, r *http.Request) {
	records, err := h.provider.Records(r.Context())
	if err != nil {
		WriteError(w, err)
		return
	}
	writeJSON(w, toEndpoints(records))
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
