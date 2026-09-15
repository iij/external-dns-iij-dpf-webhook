// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// 本ファイルは 1,000 件規模のゾーンで取得と適用が成立することを確かめる
// (T082、SC-008)。
//
// **通常の e2e 実行には含めない。** 1,000 件のレコードを作って消すため、
// 実行に数分かかり、検証用ゾーンを大きく書き換える。`DPF_E2E_SCALE=1` を
// 明示した場合にのみ走る。
//
//	DPF_E2E_SCALE          "1" のとき実行する
//	DPF_E2E_SCALE_RECORDS  作成する件数 (既定 1000)
//	DPF_E2E_READ_BUDGET    GET /records に許す時間 (既定 5s)

// 読み取りの時間予算は ExternalDNS v0.22.0 の既定値 (5s) に合わせる。
// これを超えると ExternalDNS 側が要求を打ち切る。
//
// **書き込みには時間予算を設けない。** 適用は DPF の反映完了まで待ち
// (FR-011)、dpf 層が applyTimeout (10 分) で自ら打ち切る。クライアント側の
// 待ち受け時間はこれを上回る必要があり (README は 605s を推奨)、その値を
// 予算に据えると経過時間が超えることは原理的になく、表明が常に真になる。
//
// 上流の既定 (10s) を予算にするのも誤りだった。README が「既定では足りない」
// と述べている設定を前提に落ちることになる。1,000 件の適用は実測で 10〜13 秒
// かかり、DPF の非同期反映の固定コストが支配的である。
//
// 計測そのものには意味があるため、経過時間はログに残す。README はこのジョブの
// ログを実測値の典拠として参照している。
const defaultReadBudget = 5 * time.Second

// TestScale は 1,000 件規模のゾーンでの取得と適用を確かめる (SC-008)。
func TestScale(t *testing.T) {
	if os.Getenv("DPF_E2E_SCALE") != "1" {
		t.Skip("DPF_E2E_SCALE=1 が必要です。検証用ゾーンを 1,000 件規模で書き換えます")
	}

	f := setupServer(t)

	count := envInt(t, "DPF_E2E_SCALE_RECORDS", 1000)
	readBudget := envDuration(t, "DPF_E2E_READ_BUDGET", defaultReadBudget)

	// 名前には実行ごとに変わる印を入れる。前回の失敗が残したレコードと
	// 混ざると、件数の表明が成り立たない。
	stamp := time.Now().UnixNano()
	bulk := make([]wireEndpoint, 0, count)
	for i := range count {
		bulk = append(bulk, wireEndpoint{
			DNSName:    fmt.Sprintf("e2e-scale-%d-%d.%s", stamp, i, f.zone),
			Targets:    []string{fmt.Sprintf("192.0.2.%d", i%256)},
			RecordType: "A",
			RecordTTL:  300,
		})
	}

	// **後始末を先に登録する。** 途中で失敗しても 1,000 件を残さない。
	// 残すと以降の実行で件数の表明が崩れ、検証用ゾーンが使えなくなる。
	f.cleanupRecords(t, bulk...)

	// 事前の件数。管理対象範囲に含まれるレコードのみが返る。
	baseline := len(f.records(t))
	t.Logf("事前のレコード件数: %d", baseline)

	t.Run("一括作成", func(t *testing.T) {
		elapsed := timed(func() {
			f.mustApply(t, wireChanges{Create: bulk})
		})
		t.Logf("%d 件の作成: %s", count, elapsed.Round(time.Millisecond))
	})

	t.Run("一覧の取得が待ち受け時間内に完了する", func(t *testing.T) {
		var got int
		elapsed := timed(func() {
			got = len(f.records(t))
		})
		t.Logf("%d 件の取得: %s", got, elapsed.Round(time.Millisecond))

		if got < baseline+count {
			t.Errorf("件数 = %d, want %d 以上。作成が反映されていない", got, baseline+count)
		}

		// SC-008: 1,000 件でも ExternalDNS の待ち受け時間内に完了すること。
		if elapsed > readBudget {
			t.Errorf("取得に %s かかった。予算 %s を超えている (SC-008)。"+
				"ExternalDNS 側の --webhook-provider-read-timeout を延ばす必要がある",
				elapsed.Round(time.Millisecond), readBudget)
		}
	})

	// FR-010: 同一の変更を再適用しても最終状態が変わらない。規模が大きいほど
	// マージの誤りが件数の差として現れやすい。
	t.Run("再適用で件数が変わらない", func(t *testing.T) {
		before := len(f.records(t))

		elapsed := timed(func() {
			f.mustApply(t, wireChanges{UpdateNew: bulk})
		})
		t.Logf("%d 件の再適用: %s", count, elapsed.Round(time.Millisecond))

		if after := len(f.records(t)); after != before {
			t.Errorf("件数 = %d, want %d。再適用で状態が変わった (FR-010)", after, before)
		}
	})

	t.Run("一括削除", func(t *testing.T) {
		elapsed := timed(func() {
			if status := f.apply(t, wireChanges{Delete: bulk}); status != http.StatusNoContent {
				t.Fatalf("POST /records = %d, want 204", status)
			}
		})
		t.Logf("%d 件の削除: %s", count, elapsed.Round(time.Millisecond))

		// 事前の状態へ戻ること。多くても少なくてもいけない。少なければ
		// 管理対象外のレコードを巻き込んで消している (FR-009、SC-004)。
		if after := len(f.records(t)); after != baseline {
			t.Errorf("件数 = %d, want %d (事前の状態)", after, baseline)
		}
	})
}

func timed(f func()) time.Duration {
	start := time.Now()
	f()
	return time.Since(start)
}

func envInt(t *testing.T, key string, fallback int) int {
	t.Helper()

	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		t.Fatalf("%s=%q を正の整数として解釈できません", key, v)
	}
	return n
}

func envDuration(t *testing.T, key string, fallback time.Duration) time.Duration {
	t.Helper()

	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		t.Fatalf("%s=%q を正の時間として解釈できません", key, v)
	}
	return d
}
