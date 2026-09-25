// SPDX-License-Identifier: Apache-2.0

// Package providertest は provider の境界を差し替えるための偽実装を提供する。
//
// 原則 II は「上位層のテストが実際の DPF API に到達しないこと」を求める。
// 本パッケージはそのための道具であり、テストからのみ参照される。
package providertest

import (
	"context"
	"sync"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// Backend は provider.Backend の偽実装。
//
// ゾーンとレコードを保持し、任意の呼び出しでエラーを返せる。
// 呼び出し回数を記録するため、余計な API 呼び出しが起きていないことも検証できる。
type Backend struct {
	mu sync.Mutex

	// Zones は ListZones が返すゾーン。
	Zones []provider.Zone

	// Records はゾーン名をキーとしたレコード。
	Records map[string][]provider.Record

	// ListZonesErr / ListRecordsErr / ApplyErr が非 nil ならその呼び出しは失敗する。
	ListZonesErr   error
	ListRecordsErr error
	ApplyErr       error

	// Applied は Apply に渡された変更セットを記録する。
	Applied []AppliedChange

	// 呼び出し回数。
	ListZonesCalls   int
	ListRecordsCalls int
	ApplyCalls       int
}

// AppliedChange は Apply の 1 回分の呼び出しを記録する。
type AppliedChange struct {
	Zone      provider.Zone
	ChangeSet provider.ChangeSet
}

// New は空の偽バックエンドを返す。
func New() *Backend {
	return &Backend{Records: make(map[string][]provider.Record)}
}

// WithZone はゾーンとそのレコードを登録する。
func (b *Backend) WithZone(z provider.Zone, records ...provider.Record) *Backend {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.Records == nil {
		b.Records = make(map[string][]provider.Record)
	}
	b.Zones = append(b.Zones, z)
	b.Records[z.Name.String()] = append(b.Records[z.Name.String()], records...)
	return b
}

func (b *Backend) ListZones(context.Context) ([]provider.Zone, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ListZonesCalls++
	if b.ListZonesErr != nil {
		return nil, b.ListZonesErr
	}
	out := make([]provider.Zone, len(b.Zones))
	copy(out, b.Zones)
	return out, nil
}

func (b *Backend) ListRecords(_ context.Context, zone provider.Zone) ([]provider.Record, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ListRecordsCalls++
	if b.ListRecordsErr != nil {
		return nil, b.ListRecordsErr
	}
	src := b.Records[zone.Name.String()]
	out := make([]provider.Record, len(src))
	copy(out, src)
	return out, nil
}

func (b *Backend) Apply(_ context.Context, zone provider.Zone, cs provider.ChangeSet) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ApplyCalls++
	if b.ApplyErr != nil {
		return b.ApplyErr
	}
	b.Applied = append(b.Applied, AppliedChange{Zone: zone, ChangeSet: cs})
	return nil
}

// Counts は呼び出し回数をまとめて返す。
func (b *Backend) Counts() (zones, records, applies int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.ListZonesCalls, b.ListRecordsCalls, b.ApplyCalls
}
