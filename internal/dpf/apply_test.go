// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"strings"
	"testing"

	dpfapi "github.com/iij/dpf-go"
)

// FR-006: 説明は 80 オクテット以内に収める。
//
// DPF の共通スキーマ Description が maxLength: 80 を宣言している (research R5)。
// **固定値であるため、この検査は定数に対する検査になる。** 変更セットの内容・
// ゾーン・時刻のいずれかを足した時点で、この表明が最初に落ちる (data-model 1)。
func TestApplyAttribution_WithinLimit(t *testing.T) {
	t.Parallel()

	const dpfDescriptionMaxOctets = 80

	if applyAttribution == "" {
		t.Fatal("実行者の記録が空である")
	}
	if n := len(applyAttribution); n > dpfDescriptionMaxOctets {
		t.Errorf("説明の長さ = %d オクテット, want <= %d: %q",
			n, dpfDescriptionMaxOctets, applyAttribution)
	}
}

// FR-001 / FR-002: 反映の説明は本サービスの名前を名乗る。
//
// 実際に要求へ載ることは test/e2e/attribution_test.go が DPF の履歴で確かめる。
// ここで押さえるのは値そのものである。印のラベル (005) と同じ値を指すため、
// 書き換えると両方の意味が変わる。
func TestApplyAttribution_NamesThisService(t *testing.T) {
	t.Parallel()

	if !strings.Contains(applyAttribution, "external-dns-iij-dpf-webhook") {
		t.Errorf("実行者の記録 = %q, want external-dns-iij-dpf-webhook を含む", applyAttribution)
	}
}

// 投入集合の要約は、DPF が要求を拒んだときの手がかりになる。
//
// 件数と先頭の 1 件だけを載せる。全件を載せるとログが埋まる。
func TestSetSummary_DescribesSubmittedSet(t *testing.T) {
	t.Parallel()

	set := []dpfapi.OverwriteRecordsInner{
		cur("www.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.1"),
		cur("mail.example.jp.", dpfapi.RECORDSRRTYPE_A, 300, "192.0.2.2"),
	}

	got := setSummary(set)

	for _, want := range []string{"records=2", "www.example.jp.", "ttl=300"} {
		if !strings.Contains(got, want) {
			t.Errorf("要約に %q が含まれない: %s", want, got)
		}
	}
	if strings.Contains(got, "mail.example.jp.") {
		t.Errorf("先頭以外のレコードまで載っている: %s", got)
	}
}

// TTL が null のレコードでも要約を作れる。
//
// SOA と apex NS は TTL 未指定で運用されることが多く、投入集合の先頭に
// 来やすい。ここで落ちると、失敗の原因を書き出す経路が失敗の原因になる。
func TestSetSummary_HandlesNullTTL(t *testing.T) {
	t.Parallel()

	set := []dpfapi.OverwriteRecordsInner{
		curNullTTL("example.jp.", dpfapi.RECORDSRRTYPE_SOA, "ns1.example.jp. root.example.jp. 1 2 3 4 5"),
	}

	if got := setSummary(set); !strings.Contains(got, "ttl=null") {
		t.Errorf("要約 = %s, want ttl=null を含む", got)
	}
}

// 空の投入集合でも要約は件数を返す。
func TestSetSummary_EmptySet(t *testing.T) {
	t.Parallel()

	if got := setSummary(nil); got != "records=0" {
		t.Errorf("要約 = %q, want records=0", got)
	}
}
