// Package telemetry はログ・メトリクス・トレースを提供する。
//
// 原則 V は 3 種類のテレメトリすべてを求める。本ファイルはそのうちログを扱う。
//
// 標準出力への構造化ログは常に有効であり、OTLP 送出先の設定によって停止しない。
// テレメトリ基盤が未整備または障害中の環境でも、最低限の調査手段を残すためである。
package telemetry

import (
	"context"
	"io"
	"log/slog"
	"strings"
)

// redactedPlaceholder は伏せた値の代わりに出力する文字列。
const redactedPlaceholder = "[REDACTED]"

// secretAttrKeys は値を伏せる属性キー。
//
// FR-023 は認証情報をログに出力しないことを求める。第一の防御は「そもそも渡さない」
// ことだが、渡してしまった場合に備えて出力段でも落とす。ログは一度出れば回収できない。
var secretAttrKeys = []string{
	"token",
	"authorization",
	"api_token",
	"apitoken",
	"dpf_api_token",
	"secret",
	"password",
	"credential",
}

// NewLogger は w へ構造化ログを書く slog.Logger を返す。
//
// 出力は JSON とし、レベルは level 以上を通す。秘匿すべきキーの値は伏せられる。
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:       level,
		ReplaceAttr: redactSecrets,
	})
	return slog.New(h)
}

// redactSecrets は秘匿すべき属性の値を伏せる。
func redactSecrets(_ []string, a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	for _, s := range secretAttrKeys {
		if strings.Contains(key, s) {
			return slog.String(a.Key, redactedPlaceholder)
		}
	}
	return a
}

// WithAdditionalSink は base に加えて extra へも同じレコードを書くロガーを返す。
//
// OTLP 送出を有効にしたときに用いる。base (標準出力) への出力は extra の成否に
// かかわらず行われる。extra への書き込みが失敗しても、その失敗を呼び出し側へ
// 伝播させない。テレメトリの送出失敗によって DNS レコードの処理を停止させない
// ためである (FR-024)。
func WithAdditionalSink(base, extra *slog.Logger) *slog.Logger {
	return slog.New(&teeHandler{
		primary:   base.Handler(),
		secondary: extra.Handler(),
	})
}

// teeHandler は 2 つのハンドラへレコードを配る slog.Handler。
type teeHandler struct {
	primary   slog.Handler
	secondary slog.Handler
}

func (h *teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.primary.Enabled(ctx, l) || h.secondary.Enabled(ctx, l)
}

// Handle は primary へ書いてから secondary へ書く。
//
// secondary の失敗は握り潰す。ここで返すエラーは呼び出し元の処理を止めうるが、
// テレメトリの送出失敗は本来の処理を止める理由にならない (FR-024)。
// primary の失敗のみを返す。
func (h *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	var primaryErr error
	if h.primary.Enabled(ctx, r.Level) {
		primaryErr = h.primary.Handle(ctx, r.Clone())
	}
	if h.secondary.Enabled(ctx, r.Level) {
		_ = h.secondary.Handle(ctx, r.Clone())
	}
	return primaryErr
}

func (h *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{
		primary:   h.primary.WithAttrs(attrs),
		secondary: h.secondary.WithAttrs(attrs),
	}
}

func (h *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{
		primary:   h.primary.WithGroup(name),
		secondary: h.secondary.WithGroup(name),
	}
}
