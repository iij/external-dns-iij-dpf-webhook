// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
)

// ApplyChanges は変更セットを DPF へ反映する。
//
// 手順は次のとおり。
//
//  1. 管理対象範囲による絞り込み (範囲外は除外。失敗にしない)
//  2. 書き込み先ゾーンの解決 (最長一致)
//  3. 種別ごとの検証 (DPF へ送る前に止める)
//  4. ゾーンごとの適用
//
// 範囲外のレコードを失敗ではなく除外として扱うのは、範囲外が 1 件混ざった
// だけで正当な変更まで巻き添えで止まるのを避けるためである。ExternalDNS は
// 管理対象外の名前も変更セットに含めうる。
//
// 検証の失敗は恒久的な失敗として返す。DPF へ送る前に止めることで、
// 無駄な API 呼び出しとレート制限の消費を避ける。
func (p *Provider) ApplyChanges(ctx context.Context, cs ChangeSet) error {
	if cs.IsEmpty() || p.scope.IsEmpty() {
		return nil
	}

	// 要求受信から DPF 呼び出しまでを 1 つの流れとして追跡する (FR-022)。
	// dpf 層が作る span はこの span の子として繋がる。
	ctx, span := p.tracer.Start(ctx, "provider.ApplyChanges")
	defer span.End()

	inScope := p.filterToScope(cs)
	if inScope.IsEmpty() {
		return nil
	}

	// ゾーンを知らなくても判断できる違反は、バックエンドを呼ぶ前に止める。
	// 後に置くと、DPF が落ちている間は形式違反が一時的な障害として返り、
	// ExternalDNS が通らない要求を再試行し続ける。
	if err := ValidateFormat(inScope); err != nil {
		return err
	}

	zones, err := p.backend.ListZones(ctx)
	if err != nil {
		return err
	}

	groups, err := groupByZone(inScope, zones)
	if err != nil {
		return err
	}

	for _, g := range groups {
		zone, zoneChanges := g.zone, g.changes

		p.logApply(zone, zoneChanges)

		err := p.backend.Apply(ctx, zone, zoneChanges)
		p.recordOutcome(ctx, zoneChanges, err == nil)

		if err != nil {
			// 1 つのゾーンが失敗したら全体を失敗として返す。一部だけ成功した
			// 状態を成功として返すと、ExternalDNS は失敗した側の変更も
			// 反映済みと見なす (FR-012)。
			return fmt.Errorf("ゾーン %s への適用に失敗: %w", zone.Name, err)
		}
	}

	return nil
}

// recordOutcome は適用の成否と件数を計測する (FR-021)。
//
// ラベルは操作種別と成否のみ。ゾーン名やレコード名は含めない (原則 V)。
func (p *Provider) recordOutcome(ctx context.Context, cs ChangeSet, success bool) {
	if p.metrics == nil {
		return
	}

	for op, records := range map[string][]Record{
		OpCreate: cs.Create,
		OpUpdate: cs.UpdateTo,
		OpDelete: cs.Delete,
	} {
		if len(records) > 0 {
			p.metrics.RecordChanges(ctx, op, success, len(records))
		}
	}

	if !success {
		p.metrics.RecordApplyFailure(ctx)
	}
}

// filterToScope は管理対象範囲に含まれるレコードだけを残す。
func (p *Provider) filterToScope(cs ChangeSet) ChangeSet {
	keep := func(records []Record) []Record {
		out := make([]Record, 0, len(records))
		for _, r := range records {
			if p.scope.Contains(r.Name) {
				out = append(out, r)
			}
		}
		return out
	}

	return ChangeSet{
		Create:   keep(cs.Create),
		UpdateTo: keep(cs.UpdateTo),
		Delete:   keep(cs.Delete),
	}
}

// zoneGroup は 1 つのゾーンと、そのゾーンに適用する変更のまとまり。
type zoneGroup struct {
	zone    Zone
	changes ChangeSet
}

// groupByZone は変更セットを書き込み先ゾーンごとに分ける。
//
// 一括更新はゾーン単位の操作であるため、ゾーンをまたぐ変更を 1 回では
// 適用できない。書き込み先は [zoneIndex] が決める。読み取り側の帰属判定と
// 同じ索引を使うことで、両者がずれないようにしている (research R12)。
//
// 解決できない名前があれば恒久的な失敗とする。本 provider はゾーンを
// 作成しない (FR-013)。
//
// 戻り値はゾーン名の昇順に並べる。適用順が呼び出しごとに変わると、
// ログの読み取りと障害時の再現が難しくなる。
func groupByZone(cs ChangeSet, zones []Zone) ([]zoneGroup, error) {
	idx := newZoneIndex(zones)
	grouped := make(map[dnsname.Name]*ChangeSet)
	owners := make(map[dnsname.Name]Zone)

	assign := func(r Record, pick func(*ChangeSet) *[]Record) error {
		owner, ok := idx.Owner(r.Name)
		if !ok {
			return fmt.Errorf("%w: %w: %s を含むゾーンがありません",
				ErrPermanent, ErrZoneNotFound, r.Name)
		}
		if grouped[owner.Name] == nil {
			grouped[owner.Name] = &ChangeSet{}
			owners[owner.Name] = owner
		}
		dst := pick(grouped[owner.Name])
		*dst = append(*dst, r)
		return nil
	}

	for _, r := range cs.Create {
		if err := assign(r, func(c *ChangeSet) *[]Record { return &c.Create }); err != nil {
			return nil, err
		}
	}
	for _, r := range cs.UpdateTo {
		if err := assign(r, func(c *ChangeSet) *[]Record { return &c.UpdateTo }); err != nil {
			return nil, err
		}
	}
	for _, r := range cs.Delete {
		if err := assign(r, func(c *ChangeSet) *[]Record { return &c.Delete }); err != nil {
			return nil, err
		}
	}

	out := make([]zoneGroup, 0, len(grouped))
	for name, changes := range grouped {
		out = append(out, zoneGroup{zone: owners[name], changes: *changes})
	}
	slices.SortFunc(out, func(a, b zoneGroup) int {
		return strings.Compare(a.zone.Name.String(), b.zone.Name.String())
	})
	return out, nil
}

// logApply は変更操作の内容を記録する (FR-020)。
//
// ゾーン・レコード名・操作種別・結果を残す。認証情報は含めない (FR-023)。
func (p *Provider) logApply(zone Zone, cs ChangeSet) {
	p.logger.Info("DNS レコードを変更します",
		"zone", zone.Name.String(),
		"create", recordNames(cs.Create),
		"update", recordNames(cs.UpdateTo),
		"delete", recordNames(cs.Delete),
	)
}

func recordNames(records []Record) []string {
	out := make([]string, 0, len(records))
	for _, r := range records {
		out = append(out, r.Key().String())
	}
	return out
}
