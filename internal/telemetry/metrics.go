// SPDX-License-Identifier: Apache-2.0

package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// 操作種別のラベル値。
//
// ラベルに使うため、値は有限の集合に限る。任意の文字列を渡すと基数が
// 非有界になり、時系列が際限なく増える。
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// Metrics は本サービスの計測器一式。
//
// 計測器は 1 組だけ定義する。Prometheus 形式と OTLP の双方は、この 1 組から
// 読み出される (research R8)。形式ごとに別の計測コードを書くと両者が乖離し、
// 「両形式が同一の計測値を表す」という原則 V の要件を満たせなくなる。
//
// ラベルにゾーン名・レコード名・レコード値を含めない (原則 V)。基数が非有界で
// あることに加え、/metrics は probe と同一ポートで公開されるため、これらを
// 載せると DNS 構成が無認証で読み取れてしまう。
type Metrics struct {
	changes      metric.Int64Counter
	dpfCalls     metric.Int64Counter
	dpfCallSecs  metric.Float64Histogram
	applyFailure metric.Int64Counter
}

func newMetrics(m metric.Meter) (*Metrics, error) {
	changes, err := m.Int64Counter(
		"dns_record_changes_total",
		metric.WithDescription("DPF へ適用した DNS レコード変更の件数"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: 計測器の作成に失敗: %w", err)
	}

	dpfCalls, err := m.Int64Counter(
		"dpf_api_calls_total",
		metric.WithDescription("DPF API の呼び出し回数"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: 計測器の作成に失敗: %w", err)
	}

	dpfCallSecs, err := m.Float64Histogram(
		"dpf_api_call_duration_seconds",
		metric.WithDescription("DPF API の呼び出しに要した時間"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: 計測器の作成に失敗: %w", err)
	}

	applyFailure, err := m.Int64Counter(
		"dns_apply_failures_total",
		metric.WithDescription("変更セットの適用に失敗した回数"),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry: 計測器の作成に失敗: %w", err)
	}

	return &Metrics{
		changes:      changes,
		dpfCalls:     dpfCalls,
		dpfCallSecs:  dpfCallSecs,
		applyFailure: applyFailure,
	}, nil
}

// RecordChanges はレコード変更の成否と件数を記録する (FR-021)。
func (m *Metrics) RecordChanges(ctx context.Context, op string, success bool, count int) {
	if m == nil {
		return
	}
	m.changes.Add(ctx, int64(count), metric.WithAttributes(
		attribute.String("operation", op),
		attribute.Bool("success", success),
	))
}

// RecordDPFCall は DPF API 呼び出しの成否と所要時間を記録する。
//
// operation は呼び出しの種類を表す固定の識別子であり、ゾーン名などを
// 含めてはならない。
func (m *Metrics) RecordDPFCall(ctx context.Context, operation string, success bool, seconds float64) {
	if m == nil {
		return
	}
	attrs := metric.WithAttributes(
		attribute.String("operation", operation),
		attribute.Bool("success", success),
	)
	m.dpfCalls.Add(ctx, 1, attrs)
	m.dpfCallSecs.Record(ctx, seconds, attrs)
}

// RecordApplyFailure は変更セットの適用失敗を記録する。
func (m *Metrics) RecordApplyFailure(ctx context.Context) {
	if m == nil {
		return
	}
	m.applyFailure.Add(ctx, 1)
}
