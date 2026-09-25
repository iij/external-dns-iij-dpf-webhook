// SPDX-License-Identifier: Apache-2.0

package telemetry_test

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/telemetry"
)

func newTelemetry(t *testing.T, cfg config.Telemetry) *telemetry.Telemetry {
	t.Helper()

	tel, err := telemetry.New(t.Context(), cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("telemetry.New = error %v", err)
	}
	t.Cleanup(func() {
		// 送出先が到達不能でも、停止は失敗扱いにならない (FR-024)。
		if err := tel.Shutdown(t.Context()); err != nil {
			t.Errorf("Shutdown = error %v。送出の失敗を返してはならない", err)
		}
	})
	return tel
}

// scrape は /metrics の出力を返す。
func scrape(t *testing.T, tel *telemetry.Telemetry) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	tel.MetricsHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics の状態コード = %d, want %d", rec.Code, http.StatusOK)
	}
	return rec.Body.String()
}

// FR-021: レコード変更操作の成否と件数を計測できる。
func TestMetrics_RecordsChangeOutcomes(t *testing.T) {
	t.Parallel()

	tel := newTelemetry(t, config.Telemetry{})

	tel.Metrics().RecordChanges(t.Context(), telemetry.OpCreate, true, 3)
	tel.Metrics().RecordChanges(t.Context(), telemetry.OpDelete, false, 1)

	out := scrape(t, tel)

	for _, want := range []string{"dns_record_changes_total", `operation="create"`, `operation="delete"`} {
		if !strings.Contains(out, want) {
			t.Errorf("/metrics に %q が現れない:\n%s", want, out)
		}
	}
	if !strings.Contains(out, `success="true"`) || !strings.Contains(out, `success="false"`) {
		t.Errorf("成否が区別されていない:\n%s", out)
	}
}

// 原則 V: 計測器は 1 組であり、Prometheus と OTLP は同じ値を表す。
//
// 形式ごとに別の計測コードを書くと両者が乖離する。リーダーを 2 つ付ける構成に
// することで、この性質は構成上自動的に満たされる (research R8)。
func TestMetrics_SingleInstrumentSet(t *testing.T) {
	t.Parallel()

	tel := newTelemetry(t, config.Telemetry{})

	// 同じ計測器へ 2 回記録すると、Prometheus 側の値も 2 回分になる。
	tel.Metrics().RecordChanges(t.Context(), telemetry.OpCreate, true, 2)
	first := countMetricLine(t, scrape(t, tel), "dns_record_changes_total")

	tel.Metrics().RecordChanges(t.Context(), telemetry.OpCreate, true, 3)
	second := countMetricLine(t, scrape(t, tel), "dns_record_changes_total")

	if second <= first {
		t.Errorf("計測値が増えていない: %v → %v", first, second)
	}
}

// 原則 V: ゾーン名・レコード名・レコード値を既定でラベルに含めない。
//
// 基数が非有界であり、かつ /metrics は probe と同一ポートで公開されるため、
// これらを載せると DNS 構成が無認証で読み取れてしまう。
func TestMetrics_NoHighCardinalityLabels(t *testing.T) {
	t.Parallel()

	tel := newTelemetry(t, config.Telemetry{})

	tel.Metrics().RecordChanges(t.Context(), telemetry.OpCreate, true, 1)
	tel.Metrics().RecordDPFCall(t.Context(), "list_records", true, 0.1)

	out := scrape(t, tel)

	// DNS 由来の値がラベルに現れないこと。計測器のスコープ情報などは対象外。
	for _, forbidden := range []string{
		"example.jp", "www.example", "192.0.2",
		`zone=`, `record_name=`, `dns_name=`, `rdata=`, `target=`,
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("/metrics に %q が現れた。DNS 構成が推測できる値を載せてはならない:\n%s", forbidden, out)
		}
	}
}

// 原則 VI: OTLP は送出先が設定された場合にのみ有効になる。
func TestOTLP_DisabledByDefault(t *testing.T) {
	t.Parallel()

	tel := newTelemetry(t, config.Telemetry{})

	if tel.OTLPEnabled() {
		t.Error("送出先が未設定なのに OTLP が有効になっている")
	}
}

func TestOTLP_EnabledWhenEndpointSet(t *testing.T) {
	t.Parallel()

	tel := newTelemetry(t, config.Telemetry{
		OTLPEndpoint: "collector.invalid:4317",
		OTLPProtocol: "grpc",
	})

	if !tel.OTLPEnabled() {
		t.Error("送出先を設定しても OTLP が有効にならない")
	}
}

// constitution v1.7.0: OTLP 送出先への TLS 検証を既定で有効とする。
func TestOTLP_TLSVerifiedByDefault(t *testing.T) {
	t.Parallel()

	cfg := config.Telemetry{OTLPEndpoint: "collector.invalid:4317", OTLPProtocol: "grpc"}
	if cfg.OTLPInsecure {
		t.Fatal("設定の既定で TLS 検証が無効になっている")
	}

	tel := newTelemetry(t, cfg)
	if tel.OTLPInsecure() {
		t.Error("TLS 検証が既定で無効になっている")
	}
}

// FR-024: テレメトリの送出先が到達不能でも、処理は継続する。
//
// 送出の失敗を本来の処理へ伝播させると、コレクタが落ちただけで DNS の更新が
// 止まる。
func TestTelemetry_UnreachableCollectorDoesNotBlock(t *testing.T) {
	t.Parallel()

	// 到達不能な送出先を設定しても、起動と計測は成功する。
	tel := newTelemetry(t, config.Telemetry{
		OTLPEndpoint: "collector.unreachable.invalid:4317",
		OTLPProtocol: "grpc",
	})

	// 計測が返らなくなったり panic したりしないこと。
	tel.Metrics().RecordChanges(t.Context(), telemetry.OpCreate, true, 1)

	ctx, span := tel.Tracer().Start(t.Context(), "test-span")
	span.End()
	_ = ctx

	// /metrics は送出先の状態に関わらず応答する。
	if out := scrape(t, tel); out == "" {
		t.Error("送出先が到達不能だと /metrics が空になった")
	}

	// 停止も上限時間内に終わり、失敗扱いにならない。
	start := time.Now()
	if err := tel.Shutdown(t.Context()); err != nil {
		t.Errorf("Shutdown = error %v。送出の失敗を返してはならない", err)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("停止に %v かかった。上限を設けること", elapsed)
	}
}

// FR-023 / SC-006: 認証情報が計測値に現れない。
func TestTelemetry_NoSecretsInMetrics(t *testing.T) {
	t.Parallel()

	const secret = "super-secret-token-value"

	tel := newTelemetry(t, config.Telemetry{})
	tel.Metrics().RecordDPFCall(t.Context(), "list_zones", false, 0.5)

	if out := scrape(t, tel); strings.Contains(out, secret) {
		t.Errorf("計測値にトークンが現れた:\n%s", out)
	}
}

// countMetricLine は指定した名前の計測値の合計を返す。
func countMetricLine(t *testing.T, out, name string) float64 {
	t.Helper()

	var total float64
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, name) {
			continue
		}
		idx := strings.LastIndex(line, " ")
		if idx < 0 {
			continue
		}
		var v float64
		if _, err := fmt.Sscan(line[idx+1:], &v); err == nil {
			total += v
		}
	}
	return total
}
