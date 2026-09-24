// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	dpfapi "github.com/iij/dpf-go"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// managedByLabelKey は、本サービスが管理していることを示すラベルの名前。
//
// 値には [applyAttribution] を用いる。**定数を 2 つにしない。** 004 の
// ゾーン反映の記録と同じものを指す印であり、表記が 2 つあると片方だけ直した
// 変更で食い違う (005 research R6)。
//
// 名前と値に使えるのは英数字と `.`、`-`、`_` である。値の長さは 63 文字まで。
// `managed-by` / `external-dns-iij-dpf-webhook` はこの範囲に収まる (research R1)。
const managedByLabelKey = "managed-by"

// applyManagedBy は、変更セットに含まれるレコードのラベルを印のみに設定する。
//
// **毎回上書きする。既存のラベルは保持しない。** これで制約が 2 つ構造的に
// 消える (research R2)。
//
//	ラベル 10 件の上限   : 常に 1 件になる。触れる経路が存在しない
//	ラベルのマップの共有 : 新しいマップを代入するため、元のマップに触れない
//
// 2 つ目が重要である。[merge] は反映済みレコードを逐語のまま写すため、
// ラベルのマップへの**参照**が投入集合に入る。加算方式なら写しを作る必要が
// あったが、差し替えなら元のマップに触れない。
//
// 代償は、運用者が本サービスのレコードに付けたラベルが失われることである。
// これは仕様であり (spec の利用前提条件 PC-001)、印の付いたレコードを外から
// 変更しても動作は保証しない。
//
// **変更セットに含まれないレコードには触れない** (FR-003)。001 のマージは
// 反映済みの内容を逐語コピーして土台にしており、そこを崩さない。人手のレコードを
// 巻き込まないことが、印の価値の前提である。
//
// 種別を解釈できないレコードは対象から外れる。そのようなレコードは
// [merge] が投入集合へ載せる前に恒久的な失敗として返すため、ここへは届かない。
func applyManagedBy(set []dpfapi.OverwriteRecordsInner, cs provider.ChangeSet) {
	changed := changedKeys(cs)
	if len(changed) == 0 {
		return
	}

	for i := range set {
		key := recordKey{name: set[i].Name, rrtype: set[i].Rrtype}
		if !changed[key] {
			continue
		}
		// **新しいマップを代入する。** 既存のマップを書き換えない。
		set[i].Labels = map[string]string{managedByLabelKey: applyAttribution}
	}
}

// changedKeys は変更セットのうち、投入集合へ書き込むレコードの鍵を集める。
//
// 削除は含めない。投入集合から落ちるため、ラベルを設定する対象がない。
func changedKeys(cs provider.ChangeSet) map[recordKey]bool {
	out := make(map[recordKey]bool, len(cs.Create)+len(cs.UpdateTo))

	for _, r := range append(append([]provider.Record{}, cs.Create...), cs.UpdateTo...) {
		key, err := providerKey(r)
		if err != nil {
			// 種別を解釈できないレコードは [merge] が失敗として返す。
			// ここへ届くことはないが、届いても印の対象から外すだけで済ませる。
			continue
		}
		out[key] = true
	}
	return out
}
