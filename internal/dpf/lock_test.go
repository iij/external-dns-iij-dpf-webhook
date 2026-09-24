// SPDX-License-Identifier: Apache-2.0

package dpf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/iij/dpf-go/utils"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
)

// ロックを待ち続けても、上限で打ち切られる。
//
// 上限がないと、他者がロックを保持している間、webhook の要求が applyTimeout
// (10 分) いっぱい塞がる。ExternalDNS は次の周期で再試行するため、
// 待ち続けるより早く一時的な失敗を返す方がよい。
func TestLockWait_CancelsAfterLimit(t *testing.T) {
	t.Parallel()

	ctx, wait := newLockWait(t.Context(), time.Millisecond)
	defer wait.stop()

	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("上限を過ぎても context が打ち切られない")
	}

	err := wait.err()
	if err == nil {
		t.Fatal("上限に達したことがエラーとして返らない")
	}
	if !errors.Is(err, provider.ErrTemporary) {
		t.Errorf("err = %v, want ErrTemporary", err)
	}
	// 分類は「ロックを取得できない」と揃える。時間をおけば解消する。
	if !errors.Is(err, utils.ErrStillLock) {
		t.Errorf("err = %v, want utils.ErrStillLock を含む", err)
	}
}

// ロックを取得できたら、上限は働かない。
//
// 編集と反映は上限の対象ではない。ここが崩れると、ゾーンの大きさや DPF の
// 混み具合によって、正常な適用が途中で打ち切られる。
func TestLockWait_DoesNotCancelAfterEnter(t *testing.T) {
	t.Parallel()

	ctx, wait := newLockWait(t.Context(), 20*time.Millisecond)
	defer wait.stop()

	wait.enter()

	select {
	case <-ctx.Done():
		t.Fatal("取得後に context が打ち切られた")
	case <-time.After(100 * time.Millisecond):
	}

	if err := wait.err(); err != nil {
		t.Errorf("取得できているのにエラーが返る: %v", err)
	}
}

// 上限に達した後に enter が呼ばれても、打ち切りは取り消されない。
//
// **どちらが先かを決めるのは enter と expire の排他である。** 先に上限へ
// 達した場合、編集は打ち切られた context の下で始まり、反映に至らず失敗する。
// 中途半端に成功させるより、失敗として返す方が安全である (原則 IV)。
func TestLockWait_EnterAfterExpiryKeepsCancellation(t *testing.T) {
	t.Parallel()

	ctx, wait := newLockWait(t.Context(), time.Millisecond)
	defer wait.stop()

	<-ctx.Done()
	wait.enter()

	if err := wait.err(); err == nil {
		t.Error("上限に達した事実が失われた")
	}
	if ctx.Err() == nil {
		t.Error("打ち切りが取り消された")
	}
}

// stop は見張りを止め、context を解放する。
func TestLockWait_StopReleasesContext(t *testing.T) {
	t.Parallel()

	ctx, wait := newLockWait(t.Context(), time.Hour)
	wait.enter()
	wait.stop()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Errorf("ctx.Err() = %v, want context.Canceled", ctx.Err())
	}
	if err := wait.err(); err != nil {
		t.Errorf("stop で上限に達したことになっている: %v", err)
	}
}
