// SPDX-License-Identifier: Apache-2.0

// Command webhook は IIJ DNS プラットフォームサービス (DPF) を DNS プロバイダとして
// 提供する ExternalDNS webhook provider である。
//
// ExternalDNS と同一 Pod 内のサイドカーとして動作することを前提とする。
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/server"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/telemetry"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/webhook"
)

// shutdownTimeout は停止処理に許す時間。
const shutdownTimeout = 20 * time.Second

func main() {
	if err := run(os.Args[1:]); err != nil {
		// ロガーがまだ組み立てられていない可能性があるため、標準エラーへ直接書く。
		fmt.Fprintf(os.Stderr, "起動できません: %v\n", err)
		os.Exit(1)
	}
}

// run は設定の読み込みから終了処理までを行う。
//
// 設定に不備があれば起動しない。既定値へフォールバックして動き続けることは、
// 設定漏れを見えなくするだけであり、原則 VI に反する (FR-017、FR-018)。
func run(args []string) error {
	cfg, err := config.Load(args)
	if err != nil {
		// 使い方の要求は異常ではない。表示して正常終了する。
		if errors.Is(err, config.ErrHelpRequested) {
			// 標準出力への書き込みに失敗しても、他に伝える手段がない。
			//nolint:errcheck,gosec // 使い方の出力に失敗しても取れる手段がない
			fmt.Fprint(os.Stdout, config.Usage())
			return nil
		}
		return err
	}

	logger := telemetry.NewLogger(os.Stdout, cfg.LogLevel)

	// 管理対象が空であることは異常ではないが、設定漏れの可能性が高い。
	// 黙って何もしない状態を作らないよう、起動時に知らせる (FR-002)。
	if cfg.Scope.IsEmpty() {
		logger.Warn("管理対象ドメインが設定されていません。レコードは 1 件も管理されません",
			"hint", "--domain-filter を指定してください")
	} else {
		domains := make([]string, 0, len(cfg.Scope.Domains()))
		for _, d := range cfg.Scope.Domains() {
			domains = append(domains, d.String())
		}
		logger.Info("管理対象ドメイン", "domains", domains)
	}

	// 終了シグナルで文脈を打ち切る。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	tel, err := telemetry.New(ctx, cfg.Telemetry, logger)
	if err != nil {
		return err
	}
	defer func() {
		// Shutdown は送出の失敗をエラーとして返さない。テレメトリの不調で
		// 終了処理が失敗扱いになるのを避けるため (FR-024)。
		//nolint:errcheck,gosec // Shutdown は設計上エラーを返さない
		tel.Shutdown(ctx)
	}()

	if tel.OTLPEnabled() {
		logger.Info("OTLP による送出を有効にしました",
			"endpoint", cfg.Telemetry.OTLPEndpoint,
			"protocol", cfg.Telemetry.OTLPProtocol)
		if tel.OTLPInsecure() {
			logger.Warn("OTLP 送出先への TLS 検証が無効です。既定は有効です")
		}
	}

	// トークンの供給元をここで検証する。取得できない状態で待ち受けを始めると、
	// probe は通るのに要求がすべて失敗する状態になる (FR-017)。
	backend, err := dpf.NewClient(ctx, cfg.DPF, tel.Metrics(), tel.Tracer())
	if err != nil {
		return err
	}

	p := provider.New(cfg.Scope, backend, logger)
	p.WithTelemetry(tel.Metrics(), tel.Tracer())

	srv := server.New(cfg.Server, webhook.NewHandler(p), tel.MetricsHandler())
	if err := srv.Start(ctx); err != nil {
		return err
	}
	logger.Info("待ち受けを開始しました",
		"provider", srv.ProviderAddr(),
		"exposed", srv.ExposedAddr())

	// 終了シグナル、またはリスナーの異常終了のいずれか早い方で止まる。
	var runErr error
	select {
	case <-ctx.Done():
		logger.Info("終了シグナルを受け取りました")
	case err := <-srv.Err():
		if err != nil {
			runErr = fmt.Errorf("待ち受けが異常終了しました: %w", err)
			logger.Error("待ち受けが異常終了しました", "error", err)
		}
	}

	// 停止処理には打ち切られていない文脈を使う。ctx はこの時点で完了しており、
	// そのまま渡すと処理中の要求を待たずに切ることになる。
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return errors.Join(runErr, err)
	}
	logger.Info("停止しました")

	return runErr
}
