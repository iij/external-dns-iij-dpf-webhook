package telemetry_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/telemetry"
)

// 原則 V: ログは構造化ログとし、標準出力へ出力する。
func TestNewLogger_EmitsStructuredJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(&buf, slog.LevelInfo)

	logger.Info("レコードを適用しました", "zone", "example.jp.", "count", 3)

	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &got); err != nil {
		t.Fatalf("出力が JSON として解釈できない: %v\n出力: %s", err, buf.String())
	}
	if got["msg"] != "レコードを適用しました" {
		t.Errorf("msg = %v, want %q", got["msg"], "レコードを適用しました")
	}
	if got["zone"] != "example.jp." {
		t.Errorf("zone = %v, want %q", got["zone"], "example.jp.")
	}
	if _, ok := got["time"]; !ok {
		t.Error("time が出力されていない")
	}
}

// ログレベルは設定で変更できる。
func TestNewLogger_RespectsLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level     slog.Level
		wantDebug bool
	}{
		{slog.LevelDebug, true},
		{slog.LevelInfo, false},
		{slog.LevelWarn, false},
	}

	for _, c := range cases {
		var buf bytes.Buffer
		logger := telemetry.NewLogger(&buf, c.level)
		logger.Debug("デバッグ出力")

		gotDebug := buf.Len() > 0
		if gotDebug != c.wantDebug {
			t.Errorf("level=%v で Debug の出力有無 = %v, want %v", c.level, gotDebug, c.wantDebug)
		}
	}
}

// 原則 V: 標準出力への出力は常に有効であり、他の出力先の設定によって停止しない。
//
// テレメトリ基盤が未整備または障害中の環境でも、最低限の調査手段を残すため。
func TestNewLogger_StdoutNotDisabledByOTLP(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := telemetry.NewLogger(&buf, slog.LevelInfo)

	// OTLP 送出先が設定された状態を模して、送出用ハンドラを追加する。
	var otlpSink bytes.Buffer
	logger = telemetry.WithAdditionalSink(logger, telemetry.NewLogger(&otlpSink, slog.LevelInfo))

	logger.Info("両方に出るべきメッセージ")

	if buf.Len() == 0 {
		t.Error("送出先を追加したら標準出力への出力が止まった。常時有効でなければならない")
	}
	if otlpSink.Len() == 0 {
		t.Error("追加した送出先に出力されていない")
	}
}

// 追加の送出先が失敗しても、標準出力への出力と処理の継続を妨げない (FR-024)。
func TestWithAdditionalSink_FailureDoesNotBlock(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	base := telemetry.NewLogger(&buf, slog.LevelInfo)
	logger := telemetry.WithAdditionalSink(base, telemetry.NewLogger(failingWriter{}, slog.LevelInfo))

	// panic せずに戻ること自体が期待する挙動。
	logger.Info("送出先が壊れていても記録される")

	if buf.Len() == 0 {
		t.Error("送出先の失敗により標準出力への出力が失われた")
	}
}

// FR-023: 認証情報をログに出力しない。
// 値を属性として渡してしまった場合に備え、既知の秘匿キーを伏せる。
func TestNewLogger_RedactsSecretAttributes(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-token-value"

	var buf bytes.Buffer
	logger := telemetry.NewLogger(&buf, slog.LevelInfo)
	logger.Info("接続します",
		"token", secret,
		"authorization", "Bearer "+secret,
		"dpf_api_token", secret,
	)

	out := buf.String()
	if strings.Contains(out, secret) {
		t.Errorf("秘匿すべき値がログに出力された:\n%s", out)
	}
	if !strings.Contains(out, "REDACTED") {
		t.Errorf("伏字の印が見当たらない:\n%s", out)
	}
}

// failingWriter は常に書き込みに失敗する io.Writer。
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, errWriteFailed
}

var errWriteFailed = errorString("write failed")

type errorString string

func (e errorString) Error() string { return string(e) }
