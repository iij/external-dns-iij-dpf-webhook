// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	"github.com/iij/dpf-go/utils"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// Client は DPF API へのアクセスを提供する。
//
// 本型は internal/provider が宣言する境界 (ports.go) の実装であり、
// dpf-go への依存はここから外へ出ない (原則 II)。
type Client struct {
	api *utils.Client

	metrics Recorder
	tracer  trace.Tracer
}

// Recorder は dpf 層が計測に用いる操作。
//
// internal/telemetry の実装を受け取るが、その型に依存しない。
type Recorder interface {
	RecordDPFCall(ctx context.Context, operation string, success bool, seconds float64)
}

// NewClient は設定から DPF クライアントを組み立てる。
//
// アクセストークンは常に明示的なプロバイダとして渡す。dpf-go の
// utils.NewClient() は環境変数 DPF_API_TOKEN を既定で参照するが、
// 本サービスはその経路を使わない (constitution v1.8.0)。
// metrics と tracer は nil を許す。テレメトリを設定しなくても動作する。
// テレメトリの有無で呼び出し経路が変わらないようにするため。
func NewClient(ctx context.Context, cfg config.DPF, metrics Recorder, tracer trace.Tracer) (*Client, error) {
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

	c := &Client{
		api:     api,
		metrics: metrics,
		// 既定では記録しないトレーサを使う。呼び出し側が nil 判定を
		// 書かなくて済むようにするため。
		tracer: noop.NewTracerProvider().Tracer(""),
	}
	if tracer != nil {
		c.tracer = tracer
	}
	return c, nil
}

// observe は DPF API 呼び出しを計測しつつ実行する。
//
// 所要時間と成否を記録し、呼び出しをトレースの span として残す。
// dpf-go 自体も OpenTelemetry に対応しているため、その span はここで作った
// span の子として繋がり、要求受信から DPF 呼び出しまでが 1 つの流れになる
// (FR-022)。
func (c *Client) observe(ctx context.Context, operation string, fn func(context.Context) error) error {
	ctx, span := c.tracer.Start(ctx, "dpf."+operation)
	defer span.End()

	start := time.Now()
	err := fn(ctx)

	if c.metrics != nil {
		c.metrics.RecordDPFCall(ctx, operation, err == nil, time.Since(start).Seconds())
	}
	if err != nil {
		span.RecordError(err)
	}
	return err
}

// secretManagerTokenTTL はシークレット管理サービスから取得したトークンを保持する時間。
//
// ローテーションの反映が最大でこの時間だけ遅れる。長くするほど外部への
// 問い合わせは減るが、失効したトークンを使い続ける窓が広がる。
const secretManagerTokenTTL = 30 * time.Second
