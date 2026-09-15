// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// Recorder は provider が計測に用いる操作。
//
// internal/telemetry の実装を受け取るが、その型に依存しない。テストで
// 計測を伴わずにドメインロジックを検証できるようにするため。
type Recorder interface {
	RecordChanges(ctx context.Context, op string, success bool, count int)
	RecordApplyFailure(ctx context.Context)
}

// 操作種別のラベル値。ラベルに使うため有限の集合に限る。
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// Provider は本サービスのドメインロジックを担う。
//
// 管理対象範囲の判定はここで行い、DPF へのアクセスは [Backend] に委ねる。
// この分離により、範囲判定の検証に DPF は要らない (原則 II)。
type Provider struct {
	scope   dnsname.Scope
	backend Backend
	logger  *slog.Logger

	metrics Recorder
	tracer  trace.Tracer
}

// New は範囲とバックエンドから Provider を組み立てる。
//
// scope が空の場合、いかなるレコードも管理対象にならない。これは異常ではなく、
// 設定されていない状態を表す (FR-002)。
func New(scope dnsname.Scope, backend Backend, logger *slog.Logger) *Provider {
	return &Provider{
		scope:   scope,
		backend: backend,
		logger:  logger,
		// 既定では計測もトレースも行わない。テレメトリが未設定でも
		// 分岐なしに呼び出せるようにするため。
		tracer: noop.NewTracerProvider().Tracer(""),
	}
}

// WithTelemetry は計測器とトレーサを設定する。
//
// 設定しなくても動作する。テレメトリの有無でドメインロジックの経路が
// 変わらないようにするため。
func (p *Provider) WithTelemetry(metrics Recorder, tracer trace.Tracer) {
	p.metrics = metrics
	if tracer != nil {
		p.tracer = tracer
	}
}

// Filters は管理対象ドメインを返す。
//
// 空の範囲では空スライスを返す。nil を返すと応答が null になり、
// 受け取り側が「未指定 = 全ドメイン」と解釈しうる (FR-002)。
func (p *Provider) Filters() []string {
	domains := p.scope.Domains()
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		out = append(out, d.String())
	}
	return out
}

// Records は管理対象のレコードを返す。
//
// 管理対象範囲に含まれるゾーンだけを問い合わせ、さらにその中から範囲に
// 含まれる名前だけを返す。ゾーン単位の絞り込みだけでは足りないのは、
// 範囲がゾーンより狭い場合があるためである。
//
// 範囲が空なら、バックエンドに問い合わせずに 0 件を返す。問い合わせても
// 結果を全て捨てることになり、レート制限を無駄に消費する。
func (p *Provider) Records(ctx context.Context) ([]Record, error) {
	if p.scope.IsEmpty() {
		return []Record{}, nil
	}

	ctx, span := p.tracer.Start(ctx, "provider.Records")
	defer span.End()

	zones, err := p.backend.ListZones(ctx)
	if err != nil {
		return nil, err
	}

	// 帰属の判定は書き込み側と同じ索引で行う (FR-044、research R12)。
	// 材料は全ゾーンであり、管理対象範囲で絞ったものではない。
	idx := newZoneIndex(zones)

	out := make([]Record, 0)
	for _, z := range zones {
		if !p.zoneIsRelevant(z) {
			continue
		}

		records, err := p.backend.ListRecords(ctx, z)
		if err != nil {
			// 部分的な結果を成功として返さない。欠けたレコードを
			// ExternalDNS が「存在しない」と判断すると、作り直すか
			// 既存を削除しにいく。
			return nil, fmt.Errorf("ゾーン %s のレコード取得に失敗: %w", z.Name, err)
		}

		for _, r := range records {
			if !p.scope.Contains(r.Name) {
				continue
			}
			// 読み取り元が帰属先でないレコードは返さない。権威を持つのは
			// 最長一致ゾーンの側であり、親ゾーン側に残る値 (ゾーン作成前の
			// 残骸、委任のグルー) は名前解決に影響しない。返すと同じ名前・
			// 種別が 2 件返り、その削除要求が書き込み側で子ゾーンへ振られて
			// 権威レコードを消す。
			if !idx.OwnedBy(r.Name, z) {
				continue
			}
			out = append(out, r)
		}
	}

	p.logRecords(ctx, out)

	return out, nil
}

// logRecords は ExternalDNS へ返すレコードを debug で記録する。
//
// **ExternalDNS は差分の判断をログに出さない。** どのフィールドが食い違っていると
// 見たかは、debug にしても分からない。したがって「何を返したか」をこちら側が
// 残さないと、同じ差分が繰り返し検出される状態 (SC-007) の原因を追えない。
//
// 記録するのは名前・種別・TTL・値である。ExternalDNS が出す
// 「Endpoints generated from ...」(あるべき状態) と並べれば、差分の出どころが
// フィールド単位で分かる。
//
// debug に限るのは、件数に比例して出力が増えるためである。1,000 件規模のゾーンで
// 常時これを出すと、本来のログが埋もれる。
//
// レコード名を残すことは原則 V に反しない。同原則がラベルと属性に禁じているのは
// 基数が非有界な値であり、**ログへの出力はその制限の対象外**である。FR-020 は
// 変更操作について対象を記録することを求めており、こちらはその読み取り側にあたる。
func (p *Provider) logRecords(ctx context.Context, records []Record) {
	if !p.logger.Enabled(ctx, slog.LevelDebug) {
		return
	}

	for _, r := range records {
		p.logger.DebugContext(ctx, "レコードを返します",
			"name", r.Name.String(),
			"type", r.Type.String(),
			"ttl", r.TTL,
			"values", r.Values,
		)
	}
}

// zoneIsRelevant は、そのゾーンに管理対象の名前が存在しうるかを判定する。
//
// 管理対象範囲とゾーンのいずれかがもう一方を含んでいれば、対象の名前が
// 含まれる可能性がある。両者に包含関係がなければ、そのゾーンを問い合わせる
// 必要はない。
//
//	範囲 example.jp     / ゾーン example.jp      → ゾーンが範囲に含まれる
//	範囲 example.jp     / ゾーン sub.example.jp  → ゾーンが範囲に含まれる
//	範囲 sub.example.jp / ゾーン example.jp      → 範囲がゾーンに含まれる
//	範囲 example.jp     / ゾーン other.jp        → 無関係
func (p *Provider) zoneIsRelevant(z Zone) bool {
	if z.Name.IsZero() {
		return false
	}
	for _, d := range p.scope.Domains() {
		if d.Contains(z.Name) || z.Name.Contains(d) {
			return true
		}
	}
	return false
}
