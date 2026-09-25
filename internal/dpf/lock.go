// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ロックの待ち方。
//
// lockTTL を過ぎたロックは他者が奪える。適用が異常終了してロックが残っても、
// この時間で自動的に解放される。短すぎると正常な適用の途中で奪われ、
// 長すぎると障害後の復旧が遅れる。保持中は dpf-go が自動で延長するため、
// 編集や反映が lockTTL より長くかかってもロックは切れない。
const (
	lockTTL          = 5 * time.Minute
	lockPollInterval = 500 * time.Millisecond
	lockWaitTimeout  = 60 * time.Second
)

// lockOptions はゾーン単位の排他の設定を返す。
//
// ロックが守るのは「マージの土台となる読み取り」と「書き込み」の間である。
// この 2 つの間に他者がゾーンを変更すると、その変更が古い読み取り結果で
// 上書きされて失われる (lost update)。[utils.ZoneApplier] は読み取りから
// 反映までをこの範囲に収める。
//
// ロックの範囲は 1 回の適用の内側に閉じる。webhook のレコード取得と適用は
// 独立した HTTP 要求であり、取得の後に適用が来る保証がない。両者をまたいで
// ロックを保持すると、適用が来ないままロックが残留してゾーンが操作不能になる
// (research R4)。
//
// 取得できるまで待つ ([utils.WithLockWait])。既定は待たずに諦めるが、
// 他者の適用は通常すぐ終わるため、待った方が無駄な失敗を返さずに済む。
// **待ち方は排他の性質ではなく実行 1 回ごとの判断であり**、保持期間 ([utils.WithTTL])
// とは別の種類の設定として渡す。待ち時間の上限は [lockWait] が与える。
//
// **排他の仕組みは差し替えない** ([utils.WithLocker] を使わない)。既定のレコードを
// 用いる排他は、編集中のレコードへの他ユーザからの編集を DPF が拒むため、
// **本ライブラリを使っていない相手 (管理画面や人手の操作) にも効く。** 外部の
// 仕組みに替えるとこの効果が失われ、README の利用前提条件 PC-001 が拠って立つ
// 前提が崩れる。
func lockOptions() []utils.ApplierOption {
	return []utils.ApplierOption{
		utils.WithLockOptions(utils.WithTTL(lockTTL)),
		utils.WithHoldOptions(utils.WithLockWait(lockPollInterval)),
	}
}

// lockWait は「ロックを取得できるまで待つ時間」に上限を与える。
//
// [utils.ZoneApplier.Apply] は取得の待機も反映も同じ context の下で行うため、
// 待機だけを短く縛る手段がない。上限を設けないと、他者がロックを保持している
// 間、webhook の要求が applyTimeout (10 分) いっぱい塞がる。ExternalDNS は
// 次の周期で再試行するため、待ち続けるより早く一時的な失敗を返す方がよい。
//
// 取得できた時点で待機は終わっている。**編集関数が呼ばれたことを合図に上限を
// 解除する。** 編集と反映は上限の対象ではない。
//
// 合図より前に上限へ達した場合は context を打ち切る。打ち切りは待機の中で
// 観測され、Apply は失敗として戻る。このとき err がロック取得の時間切れを
// 表すエラーを返す。context の打ち切りをそのまま返すと、原因が読めない。
//
// **上限が覆うのは、待機に続く「反映済みレコードの読み直し」までである。**
// 取得と読み直しの境目を外から観測する手段がないため、両方を 1 つの上限で
// 括っている。取得自体も書き込み内容の確認 (既定 10 秒) を含むため、
// 上限は待機だけを見た値より長く取ってある。
type lockWait struct {
	limit  time.Duration
	cancel context.CancelFunc
	timer  *time.Timer

	mu      sync.Mutex
	entered bool
	expired bool
}

// newLockWait は limit を上限とする見張りを始め、その下で使う context を返す。
//
// 呼び出し側は戻り値の [lockWait.stop] を defer すること。見張りを止め、
// 派生した context を解放する。
func newLockWait(ctx context.Context, limit time.Duration) (context.Context, *lockWait) {
	ctx, cancel := context.WithCancel(ctx)

	w := &lockWait{limit: limit, cancel: cancel}
	w.timer = time.AfterFunc(limit, w.expire)

	return ctx, w
}

// enter は待機が終わったことを告げる。以降、上限は働かない。
func (w *lockWait) enter() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.entered = true
	w.timer.Stop()
}

// expire は上限に達したときに呼ばれる。
//
// [lockWait.enter] と排他する。**先に enter が通っていれば何もしない。**
// 取得の直後に上限へ達した場合でも、始まった編集を打ち切らない。
func (w *lockWait) expire() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.entered {
		return
	}
	w.expired = true
	w.cancel()
}

// stop は見張りを止め、context を解放する。
func (w *lockWait) stop() {
	w.timer.Stop()
	w.cancel()
}

// err は上限に達していた場合に、その事実を表すエラーを返す。
//
// 分類は [utils.ErrStillLock] と揃える。待ち切れなかったことは
// 「他の操作が進行中である」ことの一形態であり、時間をおけば解消する。
func (w *lockWait) err() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.expired {
		return nil
	}
	return fmt.Errorf("%w: ゾーンのロックを %s 以内に取得できませんでした: %w",
		provider.ErrTemporary, w.limit, utils.ErrStillLock)
}
