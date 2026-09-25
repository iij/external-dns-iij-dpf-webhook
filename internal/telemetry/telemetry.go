// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	promhttp "github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// serviceName は計測値とトレースに付く識別子。
const serviceName = "external-dns-iij-dpf-webhook"

// shutdownTimeout は停止時に送出の完了を待つ時間。
//
// 送出先が到達不能な場合、待ち続けると終了できなくなる。打ち切る。
const shutdownTimeout = 5 * time.Second

// Telemetry はログ・メトリクス・トレースの提供をまとめる。
//
// 計測器は 1 組だけ作り、Prometheus 形式と OTLP の 2 つのリーダーを接続する。
// この構成により「両形式が同一の計測値を表す」という原則 V の要件が、
// 実装の注意ではなく構成によって満たされる (research R8)。
type Telemetry struct {
	metrics *Metrics
	tracer  trace.Tracer
	logger  *slog.Logger

	registry *prometheus.Registry
	shutdown []func(context.Context) error

	otlpEnabled  bool
	otlpInsecure bool
}

// New はテレメトリを組み立てる。
//
// OTLP の送出は、送出先が設定された場合にのみ有効になる (原則 VI)。
// 送出先が到達不能でも起動は成功する。テレメトリの都合で本来の処理を
// 止めないため (FR-024)。
func New(ctx context.Context, cfg config.Telemetry, logger *slog.Logger) (*Telemetry, error) {
	t := &Telemetry{
		logger:       logger,
		registry:     prometheus.NewRegistry(),
		otlpEnabled:  cfg.OTLPEnabled(),
		otlpInsecure: cfg.OTLPInsecure,
	}

	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetry: リソース情報の作成に失敗: %w", err)
	}

	if err := t.initMetrics(ctx, cfg, res); err != nil {
		return nil, err
	}
	if err := t.initTracing(ctx, cfg, res); err != nil {
		return nil, err
	}

	return t, nil
}

// initMetrics は計測器と 2 つのリーダーを組み立てる。
func (t *Telemetry) initMetrics(ctx context.Context, cfg config.Telemetry, res *resource.Resource) error {
	// Prometheus 形式は pull で読まれる。常に用意する。
	// スコープ情報とターゲット情報は出力から外す。運用上の価値がない一方で、
	// 全系列にラベルが付いて出力が読みにくくなる。
	promExporter, err := otelprom.New(
		otelprom.WithRegisterer(t.registry),
		otelprom.WithoutScopeInfo(),
		otelprom.WithoutTargetInfo(),
	)
	if err != nil {
		return fmt.Errorf("telemetry: Prometheus エクスポータの作成に失敗: %w", err)
	}

	opts := []sdkmetric.Option{
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(promExporter),
	}

	// OTLP は push。送出先が設定された場合にのみ追加する (原則 VI)。
	if cfg.OTLPEnabled() {
		reader, readerErr := t.newOTLPMetricReader(ctx, cfg)
		if readerErr != nil {
			return readerErr
		}
		opts = append(opts, sdkmetric.WithReader(reader))
	}

	provider := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(provider)
	t.shutdown = append(t.shutdown, provider.Shutdown)

	// 計測器は 1 組。上で接続した全リーダーがここから読み出す。
	t.metrics, err = newMetrics(provider.Meter(serviceName))
	return err
}

func (t *Telemetry) newOTLPMetricReader(ctx context.Context, cfg config.Telemetry) (sdkmetric.Reader, error) {
	var (
		exporter sdkmetric.Exporter
		err      error
	)

	switch cfg.OTLPProtocol {
	case "http":
		opts := []otlpmetrichttp.Option{otlpmetrichttp.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}
		exporter, err = otlpmetrichttp.New(ctx, opts...)
	default:
		opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			opts = append(opts, otlpmetricgrpc.WithInsecure())
		}
		exporter, err = otlpmetricgrpc.New(ctx, opts...)
	}
	if err != nil {
		return nil, fmt.Errorf("telemetry: OTLP メトリクスエクスポータの作成に失敗: %w", err)
	}

	return sdkmetric.NewPeriodicReader(exporter), nil
}

// initTracing はトレースを組み立てる。
//
// 送出先が未設定なら、記録しないトレーサを使う。計測箇所のコードを
// 条件分岐で汚さずに済む。
func (t *Telemetry) initTracing(ctx context.Context, cfg config.Telemetry, res *resource.Resource) error {
	if !cfg.OTLPEnabled() {
		t.tracer = otel.Tracer(serviceName)
		return nil
	}

	var (
		exporter *otlptrace.Exporter
		err      error
	)

	switch cfg.OTLPProtocol {
	case "http":
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	default:
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint)}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}
	if err != nil {
		return fmt.Errorf("telemetry: OTLP トレースエクスポータの作成に失敗: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(provider)
	t.shutdown = append(t.shutdown, provider.Shutdown)
	t.tracer = provider.Tracer(serviceName)

	return nil
}

// Metrics は計測器を返す。
func (t *Telemetry) Metrics() *Metrics { return t.metrics }

// Tracer はトレーサを返す。
//
// OTLP の送出先が未設定でも有効なトレーサを返す。呼び出し側が送出の有無で
// 分岐しなくて済むようにするため。
func (t *Telemetry) Tracer() trace.Tracer { return t.tracer }

// Logger はロガーを返す。
func (t *Telemetry) Logger() *slog.Logger { return t.logger }

// MetricsHandler は Prometheus 形式の計測値を返すハンドラ。
//
// exposed リスナーの /metrics に登録する。返す情報は計測値に限る。
func (t *Telemetry) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(t.registry, promhttp.HandlerOpts{
		// エラーの詳細を応答本文へ載せない。/metrics は認証なしで公開される。
		ErrorHandling: promhttp.HTTPErrorOnError,
	})
}

// OTLPEnabled は OTLP による送出が有効かを報告する。
func (t *Telemetry) OTLPEnabled() bool { return t.otlpEnabled }

// OTLPInsecure は OTLP 送出先への TLS 検証が無効かを報告する。
func (t *Telemetry) OTLPInsecure() bool { return t.otlpInsecure }

// Shutdown は送出の完了を待ってから停止する。
//
// 送出先が到達不能な場合に備えて待ち時間に上限を設ける。テレメトリの都合で
// 終了できなくなるのを避けるため。
//
// **送出の失敗をエラーとして返さない。** コレクタが落ちているだけで終了処理が
// 失敗扱いになると、呼び出し側がそれを障害として報告する。テレメトリの不調は
// 本来の処理の成否と無関係である (FR-024)。失敗はログに残す。
func (t *Telemetry) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	for _, fn := range t.shutdown {
		if err := fn(ctx); err != nil {
			t.logger.Warn("テレメトリの停止処理に失敗しました。処理は続行します",
				"error", err)
		}
	}
	return nil
}
