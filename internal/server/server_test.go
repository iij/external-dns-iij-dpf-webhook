package server_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/server"
)

// freePort は空いているポートを 1 つ確保して返す。
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ポートの確保に失敗: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startServer はサーバを起動し、終了処理を t に登録する。
func startServer(t *testing.T, cfg config.Server, provider http.Handler) *server.Server {
	t.Helper()

	s := server.New(cfg, provider, nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("起動に失敗: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("停止に失敗: %v", err)
		}
	})

	// 待ち受け開始を待つ。
	waitListening(t, s.ExposedAddr())
	return s
}

func waitListening(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s が待ち受け状態にならなかった", addr)
}

// FR-019: 稼働状態を外部から確認できる。
func TestServer_Healthz(t *testing.T) {
	t.Parallel()

	cfg := config.Server{
		ProviderAddr: net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := startServer(t, cfg, http.NotFoundHandler())

	resp, err := http.Get("http://" + s.ExposedAddr() + "/healthz")
	if err != nil {
		t.Fatalf("healthz への要求に失敗: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz の状態コード = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// 原則 VI: provider エンドポイントは既定でループバックのみに待ち受ける。
//
// ExternalDNS とは同一 Pod 内のサイドカーとして通信するため、Pod 外への公開は
// 既定では不要である。既定で外部に開くと、設定漏れがそのまま露出になる。
func TestServer_ProviderBindsLoopbackOnly(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	cfg := config.Server{
		ProviderAddr: net.JoinHostPort("127.0.0.1", itoa(port)),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := startServer(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	waitListening(t, s.ProviderAddr())

	// ループバックからは到達する。
	resp, err := http.Get("http://" + s.ProviderAddr() + "/")
	if err != nil {
		t.Fatalf("ループバックから provider へ到達できない: %v", err)
	}
	resp.Body.Close()

	// ループバック以外のローカルアドレスからは到達しない。
	for _, ip := range nonLoopbackIPs(t) {
		addr := net.JoinHostPort(ip, itoa(port))
		c, dialErr := net.DialTimeout("tcp", addr, 300*time.Millisecond)
		if dialErr == nil {
			c.Close()
			t.Errorf("provider が %s で待ち受けている。ループバックのみに絞らねばならない", addr)
		}
	}
}

// exposed エンドポイントが返す情報は healthz と metrics に限る。
//
// DNS 構成が推測できる情報を認証なしで返さないため、未知の経路は 404 とする。
func TestServer_ExposedServesOnlyKnownPaths(t *testing.T) {
	t.Parallel()

	cfg := config.Server{
		ProviderAddr: net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := startServer(t, cfg, http.NotFoundHandler())

	for _, path := range []string{"/", "/records", "/debug/pprof/", "/zones"} {
		resp, err := http.Get("http://" + s.ExposedAddr() + path)
		if err != nil {
			t.Fatalf("%s への要求に失敗: %v", path, err)
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			t.Errorf("exposed が %s に応答した。healthz と metrics 以外を返してはならない", path)
		}
	}
}

// provider エンドポイントに渡したハンドラが呼ばれる。
func TestServer_ProviderHandlerIsServed(t *testing.T) {
	t.Parallel()

	called := make(chan struct{}, 1)
	cfg := config.Server{
		ProviderAddr: net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := startServer(t, cfg, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	waitListening(t, s.ProviderAddr())

	resp, err := http.Get("http://" + s.ProviderAddr() + "/records")
	if err != nil {
		t.Fatalf("provider への要求に失敗: %v", err)
	}
	resp.Body.Close()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Error("provider ハンドラが呼ばれなかった")
	}
}

// 停止は両方のリスナーを閉じる。
func TestServer_ShutdownClosesBothListeners(t *testing.T) {
	t.Parallel()

	cfg := config.Server{
		ProviderAddr: net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := server.New(cfg, http.NotFoundHandler(), nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("起動に失敗: %v", err)
	}
	waitListening(t, s.ExposedAddr())
	waitListening(t, s.ProviderAddr())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("停止に失敗: %v", err)
	}

	for _, addr := range []string{s.ProviderAddr(), s.ExposedAddr()} {
		if c, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
			c.Close()
			t.Errorf("停止後も %s が待ち受けている", addr)
		}
	}
}

// 待ち受けアドレスが使用中なら、起動は失敗する。
// 起動できない状態で動き続けないため (原則 VI)。
func TestServer_StartFailsOnBusyPort(t *testing.T) {
	t.Parallel()

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("前提のリスナー作成に失敗: %v", err)
	}
	defer busy.Close()

	cfg := config.Server{
		ProviderAddr: busy.Addr().String(),
		ExposedAddr:  net.JoinHostPort("127.0.0.1", itoa(freePort(t))),
	}
	s := server.New(cfg, http.NotFoundHandler(), nil)
	if startErr := s.Start(t.Context()); startErr == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		t.Error("使用中のポートで起動に成功した")
	}
}

// nonLoopbackIPs はこのホストのループバック以外の IPv4 アドレスを返す。
func nonLoopbackIPs(t *testing.T) []string {
	t.Helper()

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Skipf("インタフェース情報を取得できない: %v", err)
	}

	var out []string
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if len(out) == 0 {
		t.Skip("ループバック以外のアドレスがないため確認を省略")
	}
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [8]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}
