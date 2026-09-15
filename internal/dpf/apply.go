// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"time"

	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// applyTimeout は 1 回の適用に許す時間。
//
// 反映の待ち合わせを含む。ゾーン全件の送信と非同期ジョブの完了待ちがあるため、
// 単発の API 呼び出しより長く取る。
const applyTimeout = 10 * time.Minute

// Apply は変更セットを zone へ適用し、反映が完了するまで待つ。
//
// 手順は contracts/dpf-client.md に従う。
//
//  1. ゾーンロックの取得
//  2. 反映済みレコードの全件取得 (マージの土台をここで読み直す)
//  3. マージ (管理対象外は逐語コピー)
//  4. 投入前ガード
//  5. 一括更新とゾーン反映 (原子的に実行)
//  6. 反映完了の待ち合わせ
//  7. ロックの解放
//
// 呼び出し側は変更セットを渡すだけでよい。ゾーンの現在の内容を読み直して
// マージする処理は、この境界の内側で完結する。
//
// 反映完了前に成功を返さない (FR-011)。途中で中止した場合も成功を返さない
// (FR-012)。更新と反映は原子的に行われるため、中止時に巻き戻す保留状態は
// そもそも発生しない。
func (c *Client) Apply(ctx context.Context, zone provider.Zone, cs provider.ChangeSet) error {
	if cs.IsEmpty() {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	return c.withZoneLock(ctx, zone, func() error {
		current, err := c.currentRecords(ctx, zone)
		if err != nil {
			return err
		}

		set, err := merge(current, cs)
		if err != nil {
			return Classify(err)
		}

		if err := guard(current, set, cs); err != nil {
			return err
		}

		return c.atomicChanges(ctx, zone, set)
	})
}

// currentRecords は反映済みレコードを生の形のまま全件返す。
//
// [Client.ListRecords] と違い、種別で絞らず変換もしない。マージの土台には
// ゾーンのあるべき全体が要るため、管理対象外のレコードも含めて受け取る。
func (c *Client) currentRecords(ctx context.Context, zone provider.Zone) ([]dpfapi.Record, error) {
	api := c.api.GetAPIClient()

	var result []dpfapi.Record
	err := c.observe(ctx, "current_records", func(ctx context.Context) error {
		return c.api.Operation(ctx, func() error {
			//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
			records, resp, err := api.RecordsAPI.GetRecordCurrents(ctx, zone.ID).ExecuteAll()
			if err != nil {
				return wrapAPIError(resp, err)
			}
			result = records.GetResults()
			return nil
		})
	})
	if err != nil {
		return nil, Classify(fmt.Errorf("ゾーン %s の反映済みレコード取得に失敗: %w", zone.Name, err))
	}
	return result, nil
}

// applyAttribution は、ゾーン反映の実行者として記録する名前。
//
// DPF はゾーン反映の履歴に、要求へ添えた説明を保持する。運用者が履歴を開いた
// とき、本サービスによる反映と人手による反映を見分けられるようにする (004)。
//
// **固定値である。** 変更セットの内容、ゾーン、時刻のいずれにも依存させない。
// DPF の共通スキーマ Description は maxLength: 80 を宣言しており、固定値なら
// 収まることが定数として言える。可変長の内容を足すと、その保証が消える
// (004 research R4/R5)。
//
// 履歴には編集者 (operator) も残るが、これは DPF が資格情報から決める値であり、
// 区別できるのは「どの DPF アカウントか」までである。人が使うアカウントと
// 本サービスのアカウントが同じ場合に区別がつかない。どのソフトウェアが実行したかは、
// そのソフトウェア自身が名乗るしかない (004 research R3)。
const applyAttribution = "external-dns-iij-dpf-webhook"

// atomicChangesBody は一括更新の要求を組み立てる。
//
// API 呼び出しから分けてあるのは、**組み立ての内容を API なしに検証できる
// ようにするため**である。説明が載ることと overwrite フラグが false のままで
// あることは、DPF へ到達せずに確かめられる。
//
// overwrite_soa と overwrite_zone_apex_ns は常に false を明示する。既定値に
// 頼らないのは、既定が変わってもゾーンの権威データがこちらの意図しない値で
// 上書きされないようにするため。
func atomicChangesBody(set []dpfapi.OverwriteRecordsInner) dpfapi.PatchZoneAtomicChanges {
	overwrite := false
	description := applyAttribution

	return dpfapi.PatchZoneAtomicChanges{
		Records:             set,
		Description:         &description,
		OverwriteSoa:        &overwrite,
		OverwriteZoneApexNs: &overwrite,
	}
}

// atomicChanges は投入集合でゾーンを一括更新し、反映の完了を待つ。
//
// overwrite_soa と overwrite_zone_apex_ns は**常に false** を明示して送る。
// これは「records に載せた SOA / apex NS の値を取り込むか」を決めるフラグで
// あり、「records から省いてよいか」ではない。records には SOA と apex NS を
// 含めなければならず、欠けると 400 (soa_not_found / apex_ns_not_found) になる
// (research R3)。
//
// 既定値に頼らず明示するのは、既定が変わってもゾーンの権威データが
// こちらの意図しない値で上書きされないようにするため。false のままなら
// FR-029 は API 側で担保される。
//
// 一括更新は非同期ジョブとして実行される。SyncWaitContext で完了を待つことで、
// 反映が済むまで成功を返さない (FR-011)。
func (c *Client) atomicChanges(ctx context.Context, zone provider.Zone, set []dpfapi.OverwriteRecordsInner) error {
	api := c.api.GetAPIClient()

	body := atomicChangesBody(set)

	// DPF が要求を拒んだとき、何を送ったのかが分からないと原因を追えない。
	// 件数と先頭の 1 件だけを添える。全件を載せるとログが埋まる。
	summary := fmt.Sprintf("records=%d", len(set))
	if len(set) > 0 {
		first := set[0]
		ttl := "null"
		if v := first.Ttl.Get(); v != nil {
			ttl = fmt.Sprintf("%d", *v)
		}
		summary = fmt.Sprintf("%s first=%q %v ttl=%s rdata=%d",
			summary, first.Name, first.Rrtype, ttl, len(first.Rdata))
	}

	err := c.observe(ctx, "atomic_changes", func(ctx context.Context) error {
		return c.api.Operation(ctx, func() error {
			//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
			async, resp, err := api.ZonesAPI.
				PatchZoneAtomicChanges(ctx, zone.ID).
				PatchZoneAtomicChanges(body).
				Execute()
			if err != nil {
				return wrapAPIError(resp, err)
			}

			//nolint:bodyclose // dpf-go が Body を閉じたうえで返すため
			_, jobResp, jobErr := api.JobsAPI.SyncWaitContext(ctx, async, resp, nil)
			if jobErr != nil {
				return wrapAPIError(jobResp, jobErr)
			}
			return nil
		})
	})
	if err != nil {
		return Classify(fmt.Errorf("ゾーン %s の適用に失敗 (%s): %w", zone.Name, summary, err))
	}

	return nil
}
