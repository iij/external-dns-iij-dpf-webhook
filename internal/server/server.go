// SPDX-License-Identifier: Apache-2.0

// Package server は 2 つの HTTP リスナーを起動・停止する。
//
// リスナーを 2 つに分けるのは、公開範囲が異なるためである。
//
//	provider (既定 127.0.0.1:8888) — ExternalDNS からの webhook 要求。
//	  同一 Pod 内のサイドカーとして通信するため、既定ではループバックのみに
//	  待ち受ける (原則 VI)。
//
//	exposed (既定 :8080) — healthz と metrics。
//	  kubelet からの probe と Prometheus のスクレイプを受けるため、Pod 外から
//	  到達できる必要がある。返す情報はこの 2 つに限る。
//
// この 2 つを同じポートに載せると、probe を通す設定が webhook API まで
// 開いてしまう。ポートを分けることで NetworkPolicy が両者を区別できる。
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
)

// readHeaderTimeout はヘッダの読み取りに許す時間。
// 無制限にすると、接続を開いたまま送らないだけでリソースを占有される。
const readHeaderTimeout = 10 * time.Second

// shutdownGrace は停止時に処理中の要求を待つ時間の上限。
const shutdownGrace = 15 * time.Second

// Server は provider と exposed の 2 つのリスナーを束ねる。
type Server struct {
	provider *http.Server
	exposed  *http.Server

	providerLn net.Listener
	exposedLn  net.Listener

	// metrics は /metrics のハンドラ。nil なら経路を作らない。
	// 計測値の提供は US4 で追加する。
	metrics http.Handler

	mu      sync.Mutex
	started bool
	errs    chan error
}

// New は設定からサーバを組み立てる。
//
// providerHandler は webhook provider API のハンドラ。metricsHandler が nil でなければ
// exposed リスナーに /metrics を追加する。
func New(cfg config.Server, providerHandler, metricsHandler http.Handler) *Server {
	s := &Server{
		metrics: metricsHandler,
		errs:    make(chan error, 2),
	}

	s.provider = &http.Server{
		Addr:              cfg.ProviderAddr,
		Handler:           providerHandler,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	s.exposed = &http.Server{
		Addr:              cfg.ExposedAddr,
		Handler:           s.exposedMux(),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	return s
}

// exposedMux は exposed リスナーの経路を組み立てる。
//
// healthz と metrics だけを登録する。未知の経路は net/http の既定により 404 になる。
// DNS 構成が推測できる情報を認証なしで返さないため、ここに他の経路を足さないこと。
func (s *Server) exposedMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// 応答本文に状態の詳細を載せない。probe に必要なのは状態コードだけであり、
		// 内部情報を無認証で公開する理由がない。
		// 書き込みに失敗しても probe 側が接続断として扱うため、ここでは何もできない。
		//nolint:errcheck,gosec // probe への書き込み失敗に対して取れる回復手段がない
		w.Write([]byte("ok\n"))
	})

	if s.metrics != nil {
		mux.Handle("/metrics", s.metrics)
	}

	return mux
}

// Start は両方のリスナーを開き、要求の処理を開始する。
//
// いずれかのリスナーを開けなければ、開いた方を閉じてエラーを返す。片方だけが
// 動いている状態で成功を返すと、probe は通るのに webhook が応答しない、
// といった半端な状態になる。
//
// ctx は待ち受けの確立にのみ用いる。確立後の要求処理は ctx の完了に影響されない。
// 停止は [Server.Shutdown] で行う。
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return errors.New("server: すでに起動しています")
	}

	var lc net.ListenConfig

	providerLn, err := lc.Listen(ctx, "tcp", s.provider.Addr)
	if err != nil {
		return fmt.Errorf("server: provider の待ち受けに失敗: %w", err)
	}

	exposedLn, err := lc.Listen(ctx, "tcp", s.exposed.Addr)
	if err != nil {
		// provider 側だけが開いた状態を残さない。閉じる際の失敗は、
		// 返すべき本来の原因を覆い隠さないよう握り潰す。
		//nolint:errcheck,gosec // 本来の原因を優先して返す
		providerLn.Close()
		return fmt.Errorf("server: exposed の待ち受けに失敗: %w", err)
	}

	s.providerLn = providerLn
	s.exposedLn = exposedLn
	s.started = true

	go s.serve(s.provider, providerLn)
	go s.serve(s.exposed, exposedLn)

	return nil
}

// serve は 1 つのリスナーで要求を処理し、正常な停止以外の終了を errs へ送る。
func (s *Server) serve(srv *http.Server, ln net.Listener) {
	err := srv.Serve(ln)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.errs <- err
		return
	}
	s.errs <- nil
}

// Err は待ち受けが異常終了した場合にエラーを受け取るチャネルを返す。
//
// 呼び出し側はこれを監視し、リスナーが落ちたら終了する。片方が落ちたまま
// 動き続けると、外形的には生きているのに機能しない状態になる。
func (s *Server) Err() <-chan error { return s.errs }

// Shutdown は処理中の要求の完了を待ってから両方のリスナーを閉じる。
//
// ctx の期限までに完了しない場合は打ち切る。
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return nil
	}
	s.started = false

	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, shutdownGrace)
		defer cancel()
	}

	// 片方の停止に失敗しても、もう片方の停止は試みる。
	perr := s.provider.Shutdown(ctx)
	eerr := s.exposed.Shutdown(ctx)

	return errors.Join(perr, eerr)
}

// ProviderAddr は provider リスナーが実際に待ち受けているアドレスを返す。
// ポートに 0 を指定した場合、割り当てられたポートを含む。
func (s *Server) ProviderAddr() string { return listenerAddr(s.providerLn, s.provider.Addr) }

// ExposedAddr は exposed リスナーが実際に待ち受けているアドレスを返す。
func (s *Server) ExposedAddr() string { return listenerAddr(s.exposedLn, s.exposed.Addr) }

func listenerAddr(ln net.Listener, fallback string) string {
	if ln == nil {
		return fallback
	}
	return ln.Addr().String()
}
