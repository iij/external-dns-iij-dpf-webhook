// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iij/external-dns-iij-dpf-webhook/internal/config"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dnsname"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/dpf"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/provider"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/server"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/telemetry"
	"github.com/iij/external-dns-iij-dpf-webhook/internal/webhook"
)

// 本ファイルは quickstart.md の DPF に依存する確認項目を機械化する (T081)。
//
// constitution v2.1.0 はこれらを **CI で実行すること**を MUST とし、手元での
// 確認をもって代えないことを MUST NOT とする。手順書に人が従う形では、
// 実行されたかどうかも、どこまで確認されたかも記録に残らない。
//
// quickstart.md のうち、ここで扱わないものと理由:
//
//	§1〜§3 (品質ゲート、ASLR、既定設定)  DPF に依存しない。ci.yml が実行する
//	§5 保留変更の破棄 (PC-004)          DPF コンソールでの人手操作が前提
//	§5 投入前ガード                      マージの誤りを外から誘発できない。
//	                                     internal/dpf の単体テストで検査する
//	§7 OTLP の送出先設定・到達不能        internal/telemetry の単体テストで検査する
//	§9 ExternalDNS サイドカー            クラスタが必要。別のワークフローで扱う (T083)
//
// **破壊的操作を行う。** 検証用ゾーンでのみ実行すること。

// 転送形式は上流仕様 (api/webhook.yaml v0.22.0) が定める。
//
// internal/webhook の型は非公開であり、また意図的に写しを持つ。ここが
// 実装と同じ型を使うと、実装側でフィールド名を変えてもテストが追随して
// しまい、契約の変更を検出できない (原則 I)。
type wireEndpoint struct {
	DNSName    string   `json:"dnsName"`
	Targets    []string `json:"targets"`
	RecordType string   `json:"recordType"`
	RecordTTL  int64    `json:"recordTTL,omitempty"`
}

type wireChanges struct {
	Create    []wireEndpoint `json:"create"`
	UpdateOld []wireEndpoint `json:"updateOld"`
	UpdateNew []wireEndpoint `json:"updateNew"`
	Delete    []wireEndpoint `json:"delete"`
}

// fixture は実際に起動した webhook サーバと、その検証用ゾーン。
type fixture struct {
	zone dnsname.Name

	// providerURL は webhook provider API の基底 URL。
	providerURL string

	// exposedURL は healthz と metrics の基底 URL。
	exposedURL string

	// token はトークン文字列。ログや metrics への漏洩を検査するために保持する
	// (SC-006)。**表明の対象としてのみ用い、出力しないこと。**
	token string

	// logs はサーバが書いたログの蓄積。漏洩の検査に使う。
	logs *syncBuffer
}

// syncBuffer は複数の goroutine から書かれるログを溜める。
//
// slog のハンドラはサーバの goroutine から呼ばれるため、bytes.Buffer を
// そのまま渡すとデータ競合になる。
type syncBuffer struct {
	mu  chan struct{}
	buf bytes.Buffer
}

func newSyncBuffer() *syncBuffer {
	return &syncBuffer{mu: make(chan struct{}, 1)}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu <- struct{}{}
	defer func() { <-b.mu }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu <- struct{}{}
	defer func() { <-b.mu }()
	return b.buf.String()
}

// setupServer は検証用ゾーンに対する webhook サーバを実際に起動する。
//
// cmd/webhook と同じ配線を通す。ハンドラだけを直接呼ぶ形にすると、
// サーバの経路分離 (provider と exposed) と probe の経路が検証されない。
//
// 待ち受けは 127.0.0.1 のポート 0 とする。既定のポートを使うと、同一ホストで
// 別の実行と衝突する。
func setupServer(t *testing.T) *fixture {
	t.Helper()

	tokenFile := os.Getenv("DPF_E2E_TOKEN_FILE")
	zoneName := os.Getenv("DPF_E2E_ZONE")
	if tokenFile == "" || zoneName == "" {
		t.Skip("DPF_E2E_TOKEN_FILE と DPF_E2E_ZONE が必要です。検証用ゾーンでのみ実行してください")
	}

	zone, err := dnsname.Parse(zoneName)
	if err != nil {
		t.Fatalf("DPF_E2E_ZONE を解釈できません: %v", err)
	}

	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("トークンファイルを読めません: %v", err)
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		t.Fatal("トークンファイルが空です")
	}

	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	t.Cleanup(cancel)

	logs := newSyncBuffer()
	logger := telemetry.NewLogger(logs, slog.LevelDebug)

	// OTLP は設定しない。既定では送出しないことが原則 VI であり、
	// ここで送出先を与えると既定の姿を検証しなくなる。
	tel, err := telemetry.New(ctx, config.Telemetry{}, logger)
	if err != nil {
		t.Fatalf("テレメトリの初期化に失敗: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		//nolint:errcheck,gosec // 後始末
		tel.Shutdown(shutdownCtx)
	})

	if tel.OTLPEnabled() {
		t.Fatal("送出先を設定していないのに OTLP が有効になっている (原則 VI)")
	}

	backend, err := dpf.NewClient(ctx, config.DPF{TokenFile: tokenFile}, tel.Metrics(), tel.Tracer())
	if err != nil {
		t.Fatalf("DPF クライアントの作成に失敗: %v", err)
	}

	p := provider.New(dnsname.NewScope(zone), backend, tel.Logger())
	p.WithTelemetry(tel.Metrics(), tel.Tracer())

	srv := server.New(config.Server{
		ProviderAddr: "127.0.0.1:0",
		ExposedAddr:  "127.0.0.1:0",
	}, webhook.NewHandler(p), tel.MetricsHandler())

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("サーバの起動に失敗: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		//nolint:errcheck,gosec // 後始末
		srv.Shutdown(shutdownCtx)
	})

	return &fixture{
		zone:        zone,
		providerURL: "http://" + srv.ProviderAddr(),
		exposedURL:  "http://" + srv.ExposedAddr(),
		token:       token,
		logs:        logs,
	}
}

// do は webhook provider API へ要求を送る。
//
// Accept にはメディアタイプを指定する。ExternalDNS がそうするため、
// 検証も同じ条件で行う。
func (f *fixture) do(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()

	var r io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("要求の符号化に失敗: %v", err)
		}
		r = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, f.providerURL+path, r)
	if err != nil {
		t.Fatalf("要求の作成に失敗: %v", err)
	}
	req.Header.Set("Accept", webhook.MediaType)
	if body != nil {
		req.Header.Set("Content-Type", webhook.MediaType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s に失敗: %v", method, path, err)
	}
	defer resp.Body.Close() //nolint:errcheck // 読み切った後の後始末

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("応答本文の読み取りに失敗: %v", err)
	}
	return resp.StatusCode, got
}

// records は GET /records の結果を返す。
func (f *fixture) records(t *testing.T) []wireEndpoint {
	t.Helper()

	status, body := f.do(t, http.MethodGet, "/records", nil)
	if status != http.StatusOK {
		t.Fatalf("GET /records = %d, want 200", status)
	}

	var out []wireEndpoint
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("応答を解釈できません: %v: %s", err, body)
	}
	return out
}

// find は一覧から名前と種別で 1 件を探す。
//
// **名前は [dnsname.Name] どうしで比べる。文字列として比べない。** 応答が返す
// のは ExternalDNS へ渡す表記 (末尾ドットなし) であり、検証側が組み立てる名前は
// 正準名 (末尾ドットあり) である。素の文字列一致で照合すると、表記の違いだけで
// 「反映されていない」と読めてしまう。[dnsname.Name.Unqualified] の godoc が
// 「戻り値を判定や比較に使わないこと。比較は Name どうしで行う」と定めている。
func (f *fixture) find(t *testing.T, name, rrtype string) (wireEndpoint, bool) {
	t.Helper()

	want, err := dnsname.Parse(name)
	if err != nil {
		t.Fatalf("探す名前を解釈できません: %v: %q", err, name)
	}

	for _, e := range f.records(t) {
		if e.RecordType != rrtype {
			continue
		}
		got, parseErr := dnsname.Parse(e.DNSName)
		if parseErr != nil {
			continue
		}
		if got == want {
			return e, true
		}
	}
	return wireEndpoint{}, false
}

// apply は POST /records を送り、状態コードを返す。
func (f *fixture) apply(t *testing.T, c wireChanges) int {
	t.Helper()

	status, body := f.do(t, http.MethodPost, "/records", c)
	if status >= 400 {
		// 本文にエラーの詳細は載らない設計だが、状態コードだけでは
		// 原因が分からない。載っていれば残す。
		t.Logf("POST /records = %d: %s", status, bytes.TrimSpace(body))
	}
	return status
}

// mustApply は POST /records が 204 で成功することを要求する。
func (f *fixture) mustApply(t *testing.T, c wireChanges) {
	t.Helper()

	// 204 No Content であること。上流仕様がこの値を定めており 200 ではない。
	if status := f.apply(t, c); status != http.StatusNoContent {
		// **想定外の失敗である。原因を出す。**
		f.dumpProviderLog(t, providerLogTailLines)
		t.Fatalf("POST /records = %d, want 204 No Content", status)
	}
}

// providerLogTailLines は失敗時に出すログの行数。
//
// 原因は直近にある。全文を出すと CI の出力が埋まり、かえって読めない。
const providerLogTailLines = 60

// dumpProviderLog は取り込んだサーバのログの末尾を出力する。
//
// **DPF の応答全文はここにしかない。** HTTP 応答の本文には設計上詳細を載せない
// ため (contracts/webhook-api.md)、状態コードだけでは何が拒否されたのか追えない。
// constitution v2.2.0 が外部 API のエラー応答を切り詰めずに記録することを MUST と
// した理由 (request_id を失わない) が効くのは、まさにこの経路である。
//
// これがなかった間、実環境で落ちた原因を「送った形と通った事例の差分」から
// 推論するしかなかった。推論は当たることもあるが、根拠にはならない。
//
// **トークンは伏せて出す。** 万一ログへ漏れていた場合に、CI の出力へ広げない。
// 漏洩そのものの検出は TestObservability が担う。ここは二重の防壁である。
func (f *fixture) dumpProviderLog(t *testing.T, lines int) {
	t.Helper()

	log := f.logs.String()
	if log == "" {
		t.Log("provider のログは空である")
		return
	}

	if f.token != "" {
		log = strings.ReplaceAll(log, f.token, "<伏せた: トークン>")
	}

	all := strings.Split(strings.TrimRight(log, "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	t.Logf("provider のログ (末尾 %d 行):\n%s", len(all), strings.Join(all, "\n"))
}

// uniqueName は実行ごとに異なる名前を返す。
//
// 前回の失敗が残したレコードと衝突しないようにする。
func (f *fixture) uniqueName(t *testing.T, prefix string) string {
	t.Helper()

	return fmt.Sprintf("%s-%d.%s", prefix, time.Now().UnixNano(), f.zone)
}

// cleanupRecords は後始末として削除を試みる。
//
// 途中でどこで失敗しても、検証用ゾーンにレコードを残さない。
func (f *fixture) cleanupRecords(t *testing.T, endpoints ...wireEndpoint) {
	t.Helper()

	t.Cleanup(func() {
		req, err := json.Marshal(wireChanges{Delete: endpoints})
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()

		//nolint:noctx // ctx は明示的に渡している
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, f.providerURL+"/records", bytes.NewReader(req))
		if err != nil {
			return
		}
		r.Header.Set("Accept", webhook.MediaType)
		r.Header.Set("Content-Type", webhook.MediaType)

		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return
		}
		//nolint:errcheck // 後始末
		resp.Body.Close()
	})
}

// TestWebhookContract は webhook 契約の 4 経路が実際の DPF に対して
// 成立することを確かめる (quickstart §4、§5、原則 I)。
func TestWebhookContract(t *testing.T) {
	f := setupServer(t)

	t.Run("GET / は管理対象ドメインを返す", func(t *testing.T) {
		status, body := f.do(t, http.MethodGet, "/", nil)
		if status != http.StatusOK {
			t.Fatalf("GET / = %d, want 200", status)
		}

		var got struct {
			Filters []string `json:"filters"`
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("応答を解釈できません: %v: %s", err, body)
		}
		if !slices.Contains(got.Filters, f.zone.String()) {
			t.Errorf("filters = %v, want %s を含む", got.Filters, f.zone)
		}
	})

	t.Run("GET /records は正規化名と対応種別のみを返す", func(t *testing.T) {
		for _, e := range f.records(t) {
			// 返される名前は ExternalDNS へ渡す表記 (小文字・**末尾ドットなし**)
			// であること。ExternalDNS の TXT レジストリは所有権レコードの有無を
			// 素の文字列一致で照合し、生成側は末尾ドットを持たない。ドット付きで
			// 返すと照合が必ず外れ、差分が永久に振動する (SC-007)。
			if e.DNSName != strings.ToLower(e.DNSName) {
				t.Errorf("%q に大文字が含まれる。正規化されていない", e.DNSName)
			}
			if strings.HasSuffix(e.DNSName, ".") {
				t.Errorf("%q が末尾ドットで終わる。ExternalDNS の表記でない (SC-007)", e.DNSName)
			}
			// 表記を落としても名前として解釈できること。
			if _, err := dnsname.Parse(e.DNSName); err != nil {
				t.Errorf("%q を名前として解釈できない: %v", e.DNSName, err)
			}

			// 許可リスト外の種別を返さない (FR-027)。SOA や CAA を返すと
			// ExternalDNS がそれらを管理対象として扱う。
			if !provider.IsSupportedRecordType(e.RecordType) {
				t.Errorf("%s %s: 対応外の種別が一覧に含まれている (FR-027)", e.DNSName, e.RecordType)
			}

			// 管理対象範囲の外の名前を返さない (FR-006)。
			name, err := dnsname.Parse(e.DNSName)
			if err != nil {
				t.Errorf("%q を正規化名として解釈できない", e.DNSName)
				continue
			}
			if !dnsname.NewScope(f.zone).Contains(name) {
				t.Errorf("%s は管理対象範囲 %s の外にある (FR-006)", e.DNSName, f.zone)
			}
		}
	})

	t.Run("空の変更セットは 204 で成功する", func(t *testing.T) {
		f.mustApply(t, wireChanges{})
	})

	t.Run("POST /adjustendpoints は 200 を返す", func(t *testing.T) {
		status, body := f.do(t, http.MethodPost, "/adjustendpoints", []wireEndpoint{{
			DNSName:    "adjust." + f.zone.String(),
			Targets:    []string{"192.0.2.1"},
			RecordType: "A",
			RecordTTL:  1, // DPF の下限。補正されないこと
		}})
		if status != http.StatusOK {
			t.Fatalf("POST /adjustendpoints = %d, want 200", status)
		}

		var got []wireEndpoint
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("応答を解釈できません: %v: %s", err, body)
		}
		if len(got) != 1 {
			t.Fatalf("件数 = %d, want 1", len(got))
		}

		// 調整は冪等でなければならない。二度目で変わるなら、ExternalDNS は
		// 同じ差分を検出し続ける (FR-015、SC-007)。
		status, again := f.do(t, http.MethodPost, "/adjustendpoints", got)
		if status != http.StatusOK {
			t.Fatalf("2 回目の POST /adjustendpoints = %d, want 200", status)
		}
		var second []wireEndpoint
		if err := json.Unmarshal(again, &second); err != nil {
			t.Fatalf("応答を解釈できません: %v", err)
		}
		if !slices.Equal(got[0].Targets, second[0].Targets) || got[0].RecordTTL != second[0].RecordTTL {
			t.Errorf("調整が冪等でない: %+v → %+v (FR-015)", got[0], second[0])
		}
	})

	t.Run("ネゴシエートできない Accept は 406", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.providerURL+"/", nil)
		if err != nil {
			t.Fatalf("要求の作成に失敗: %v", err)
		}
		req.Header.Set("Accept", "application/external.dns.webhook+json;version=99")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("要求に失敗: %v", err)
		}
		//nolint:errcheck // 状態コードのみを見る
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotAcceptable {
			t.Errorf("状態コード = %d, want 406", resp.StatusCode)
		}
	})
}

// TestNameFormsAreEquivalent は名前の表現が違っても同一のレコードとして
// 扱われることを確かめる (quickstart §4、FR-004)。
//
// ExternalDNS が送る名前の表現は上流の実装に依存する。末尾ドットや
// 大文字小文字の違いで別のレコードとして扱うと、作成と削除が噛み合わずに
// レコードが増え続ける。
func TestNameFormsAreEquivalent(t *testing.T) {
	f := setupServer(t)

	canonical := f.uniqueName(t, "e2e-forms")
	noTrailingDot := strings.TrimSuffix(canonical, ".")
	upper := strings.ToUpper(noTrailingDot)

	ep := func(name, value string) wireEndpoint {
		return wireEndpoint{DNSName: name, Targets: []string{value}, RecordType: "A", RecordTTL: 300}
	}

	f.cleanupRecords(t, ep(canonical, "192.0.2.1"))

	// 末尾ドットなしで作成する。
	f.mustApply(t, wireChanges{Create: []wireEndpoint{ep(noTrailingDot, "192.0.2.1")}})

	got, ok := f.find(t, canonical, "A")
	if !ok {
		t.Fatalf("%s が反映されていない。末尾ドットなしの名前が正規化されていない", canonical)
	}
	if !slices.Contains(got.Targets, "192.0.2.1") {
		t.Errorf("値 = %v, want 192.0.2.1 を含む", got.Targets)
	}

	// 大文字混じりで更新する。別のレコードが増えてはならない。
	f.mustApply(t, wireChanges{UpdateNew: []wireEndpoint{ep(upper, "192.0.2.99")}})

	matched := 0
	for _, e := range f.records(t) {
		if strings.EqualFold(strings.TrimSuffix(e.DNSName, "."), noTrailingDot) && e.RecordType == "A" {
			matched++
		}
	}
	if matched != 1 {
		t.Errorf("同名の A レコードが %d 件ある, want 1。表現の違いで別レコードになっている", matched)
	}

	got, ok = f.find(t, canonical, "A")
	if !ok {
		t.Fatal("大文字混じりの更新でレコードが消えた")
	}
	if !slices.Contains(got.Targets, "192.0.2.99") {
		t.Errorf("値 = %v, want 192.0.2.99 を含む。大文字混じりの名前が同一視されていない", got.Targets)
	}

	// 大文字混じりで削除する。
	f.mustApply(t, wireChanges{Delete: []wireEndpoint{ep(upper, "192.0.2.99")}})

	if _, ok := f.find(t, canonical, "A"); ok {
		t.Error("大文字混じりの削除でレコードが消えていない")
	}
}

// TestTXTRoundTrip は TXT の character-string の扱いを確かめる
// (quickstart §5、FR-032/FR-032a/FR-032b)。
//
// DPF が 255 オクテットを超える character-string を拒否するため、
// 自動分割が効いていなければ適用そのものが失敗する。
func TestTXTRoundTrip(t *testing.T) {
	f := setupServer(t)

	cases := []struct {
		name  string
		value string
		// wantStrings は読み戻した値を character-string に分解した結果に
		// 期待する内容。nil なら件数と内容を問わない。
		wantStrings []string
	}{
		{
			// 256 オクテットの 1 個。分割されて登録されること (FR-032)。
			name:  "e2e-txt-long",
			value: strings.Repeat("a", 256),
			// 255 + 1 に分かれる。連結すれば元に戻る。
			wantStrings: []string{strings.Repeat("a", 255), "a"},
		},
		{
			// 引用符で区切った複数の character-string。合計が 255 を
			// 超えてもよい (FR-032)。
			name:        "e2e-txt-multi",
			value:       `"` + strings.Repeat("b", 200) + `" "` + strings.Repeat("c", 200) + `"`,
			wantStrings: []string{strings.Repeat("b", 200), strings.Repeat("c", 200)},
		},
		{
			// 引用符を含まない値は 1 個として扱う。空白で分割すると
			// 受信側の連結で空白が失われ、値の意味が変わる (FR-032b)。
			name:        "e2e-txt-spf",
			value:       "v=spf1 -all",
			wantStrings: []string{"v=spf1 -all"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			name := f.uniqueName(t, tc.name)
			ep := wireEndpoint{
				DNSName:    name,
				Targets:    []string{tc.value},
				RecordType: "TXT",
				RecordTTL:  300,
			}
			f.cleanupRecords(t, ep)

			f.mustApply(t, wireChanges{Create: []wireEndpoint{ep}})

			first, ok := f.find(t, name, "TXT")
			if !ok {
				t.Fatalf("%s TXT が反映されていない", name)
			}
			if len(first.Targets) != 1 {
				t.Fatalf("targets = %v, want 1 件", first.Targets)
			}

			got, err := provider.SplitTXT(first.Targets[0])
			if err != nil {
				t.Fatalf("読み戻した値を解釈できません: %v: %q", err, first.Targets[0])
			}
			if tc.wantStrings != nil && !slices.Equal(got, tc.wantStrings) {
				t.Errorf("character-string = %d 件 %q, want %d 件 %q",
					len(got), summarize(got), len(tc.wantStrings), summarize(tc.wantStrings))
			}

			// FR-032a: 読み戻した値をそのまま書き戻しても分割位置が変わらない。
			// 変わると ExternalDNS が同じ差分を検出し続ける (SC-007)。
			f.mustApply(t, wireChanges{UpdateNew: []wireEndpoint{{
				DNSName:    name,
				Targets:    first.Targets,
				RecordType: "TXT",
				RecordTTL:  300,
			}}})

			second, ok := f.find(t, name, "TXT")
			if !ok {
				t.Fatal("書き戻しでレコードが消えた")
			}
			if !slices.Equal(first.Targets, second.Targets) {
				t.Errorf("書き戻しで表現が変わった (FR-032a):\n 1 回目 %q\n 2 回目 %q",
					first.Targets, second.Targets)
			}
		})
	}
}

// summarize は長い character-string を読める長さに縮める。
// 表明の失敗メッセージに 255 文字の羅列を出しても原因が分からない。
func summarize(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if len(v) > 24 {
			out = append(out, fmt.Sprintf("%s...(%d オクテット)", v[:24], len(v)))
			continue
		}
		out = append(out, v)
	}
	return out
}

// TestRejectedBeforeReachingDPF は形式違反が 4xx で拒否されることを確かめる
// (quickstart §5、FR-029/FR-030/FR-031)。
//
// 4xx であることが要点。5xx を返すと ExternalDNS は再試行するが、形式違反は
// 何度送っても通らない。
func TestRejectedBeforeReachingDPF(t *testing.T) {
	f := setupServer(t)

	apex := f.zone.String()

	cases := []struct {
		name    string
		changes wireChanges
	}{
		{
			// FR-031: DPF は A / AAAA の名前にアンダースコアを許さない。
			name: "A の名前にアンダースコア",
			changes: wireChanges{Create: []wireEndpoint{{
				DNSName: "under_score." + apex, Targets: []string{"192.0.2.1"},
				RecordType: "A", RecordTTL: 300,
			}}},
		},
		{
			// FR-030: CNAME は他の種別と同じ名前に共存できない。
			name: "CNAME と A が同名",
			changes: wireChanges{Create: []wireEndpoint{
				{DNSName: "clash." + apex, Targets: []string{"target." + apex}, RecordType: "CNAME", RecordTTL: 300},
				{DNSName: "clash." + apex, Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300},
			}},
		},
		{
			// FR-030: CNAME は同一の名前に複数の値を持てない。
			name: "CNAME に複数の値",
			changes: wireChanges{Create: []wireEndpoint{{
				DNSName: "multi." + apex, Targets: []string{"a." + apex, "b." + apex},
				RecordType: "CNAME", RecordTTL: 300,
			}}},
		},
		{
			// FR-029: ゾーン apex の NS は変更しない。作成・更新・削除の
			// いずれも受け付けない。overwrite_zone_apex_ns を常に false で
			// 送るため、受け付けても適用されない。
			name: "apex NS の作成",
			changes: wireChanges{Create: []wireEndpoint{{
				DNSName: apex, Targets: []string{"ns1.example.jp."}, RecordType: "NS", RecordTTL: 300,
			}}},
		},
		{
			name: "apex NS の更新",
			changes: wireChanges{UpdateNew: []wireEndpoint{{
				DNSName: apex, Targets: []string{"ns1.example.jp."}, RecordType: "NS", RecordTTL: 300,
			}}},
		},
		{
			name: "apex NS の削除",
			changes: wireChanges{Delete: []wireEndpoint{{
				DNSName: apex, Targets: []string{"ns1.example.jp."}, RecordType: "NS", RecordTTL: 300,
			}}},
		},
	}

	// 拒否がゾーンを変えていないことを確かめるため、前後で一覧を比べる。
	before := index(f.records(t))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status := f.apply(t, tc.changes)
			if status < 400 || status >= 500 {
				t.Errorf("状態コード = %d, want 4xx。恒久的な失敗として返していない", status)
			}
		})
	}

	after := index(f.records(t))
	for key, want := range before {
		got, ok := after[key]
		if !ok {
			t.Errorf("%s が消えた。拒否された要求がゾーンを変えている", key)
			continue
		}
		if !slices.Equal(want.Targets, got.Targets) {
			t.Errorf("%s が変化した: %v → %v", key, want.Targets, got.Targets)
		}
	}
	if len(after) != len(before) {
		t.Errorf("レコード件数 = %d, want %d。拒否された要求がゾーンを変えている", len(after), len(before))
	}
}

// index は一覧を名前と種別で引ける形にする。
func index(endpoints []wireEndpoint) map[string]wireEndpoint {
	m := make(map[string]wireEndpoint, len(endpoints))
	for _, e := range endpoints {
		m[e.DNSName+"/"+e.RecordType] = e
	}
	return m
}

// TestOutOfScopeIgnored は管理対象外の名前を含む変更セットが、
// 範囲内の変更を妨げないことを確かめる (quickstart §5、FR-009)。
//
// 範囲外を失敗として扱うと、1 件混ざっただけで正当な変更まで止まる。
// ExternalDNS は管理対象外の名前も変更セットに含めうる。
func TestOutOfScopeIgnored(t *testing.T) {
	f := setupServer(t)

	inScope := f.uniqueName(t, "e2e-inscope")
	ep := wireEndpoint{DNSName: inScope, Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300}
	f.cleanupRecords(t, ep)

	// 範囲外の名前を混ぜる。解決できるゾーンがないため、失敗として扱うと
	// 全体が 4xx になる。
	f.mustApply(t, wireChanges{Create: []wireEndpoint{
		ep,
		{DNSName: "www.out-of-scope.invalid.", Targets: []string{"192.0.2.2"}, RecordType: "A", RecordTTL: 300},
	}})

	if _, ok := f.find(t, inScope, "A"); !ok {
		t.Errorf("%s が反映されていない。範囲外の 1 件が正当な変更を止めている (FR-009)", inScope)
	}
	if _, ok := f.find(t, "www.out-of-scope.invalid.", "A"); ok {
		t.Error("範囲外のレコードが作られている")
	}
}

// TestListRecordsDoesNotHoldLock は取得要求がゾーンロックを残さないことを
// 確かめる (quickstart §5「ロックの範囲」)。
//
// 取得がロックを取って保持すると、適用が来ないままゾーンが操作不能になる。
// Records と ApplyChanges は別の要求であり、両者を跨ぐロックは持てない。
func TestListRecordsDoesNotHoldLock(t *testing.T) {
	f := setupServer(t)

	// 取得のみを数回行う。ロックを取る実装ならここで残留する。
	for range 3 {
		f.records(t)
	}

	// 直後に適用が成功すること。ロックが残っていれば ErrStillLock により
	// 一時的な失敗 (5xx) になる。
	name := f.uniqueName(t, "e2e-lock")
	ep := wireEndpoint{DNSName: name, Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300}
	f.cleanupRecords(t, ep)

	f.mustApply(t, wireChanges{Create: []wireEndpoint{ep}})
	f.mustApply(t, wireChanges{Delete: []wireEndpoint{ep}})
}

// TestObservability は probe と計測値の経路を確かめる (quickstart §7)。
//
// 秘匿情報が出力に現れないことを併せて検査する (SC-006)。ここが崩れると、
// /metrics は probe と同一ポートで無認証に公開されるため被害が大きい。
func TestObservability(t *testing.T) {
	f := setupServer(t)

	get := func(t *testing.T, url string) (int, string) {
		t.Helper()

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("要求の作成に失敗: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %s に失敗: %v", url, err)
		}
		defer resp.Body.Close() //nolint:errcheck // 読み切った後の後始末

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("応答本文の読み取りに失敗: %v", err)
		}
		return resp.StatusCode, string(body)
	}

	t.Run("healthz は稼働中に 200 を返す", func(t *testing.T) {
		if status, _ := get(t, f.exposedURL+"/healthz"); status != http.StatusOK {
			t.Errorf("GET /healthz = %d, want 200", status)
		}
	})

	// 変更を 1 件行い、計測値に反映されることを確かめる (FR-021)。
	name := f.uniqueName(t, "e2e-metrics")
	ep := wireEndpoint{DNSName: name, Targets: []string{"192.0.2.1"}, RecordType: "A", RecordTTL: 300}
	f.cleanupRecords(t, ep)

	f.mustApply(t, wireChanges{Create: []wireEndpoint{ep}})
	f.mustApply(t, wireChanges{Delete: []wireEndpoint{ep}})

	status, body := get(t, f.exposedURL+"/metrics")
	if status != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", status)
	}

	t.Run("計測値が Prometheus 形式で取得できる", func(t *testing.T) {
		for _, want := range []string{
			"dns_record_changes_total",
			"dpf_api_calls_total",
			"dpf_api_call_duration_seconds",
		} {
			if !strings.Contains(body, want) {
				t.Errorf("/metrics に %s が現れない (FR-021)", want)
			}
		}
	})

	t.Run("計測値にゾーン名とレコード名が含まれない", func(t *testing.T) {
		// /metrics は probe と同一ポートで無認証に公開される。
		// ここにゾーン名やレコード名を載せると DNS 構成が読み取れてしまう
		// (原則 V)。基数が非有界になる問題も併せて避けている。
		for _, forbidden := range []string{
			f.zone.String(),
			strings.TrimSuffix(f.zone.String(), "."),
			name,
			"192.0.2.1",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("/metrics に %q が現れる。ゾーン名・レコード名・値をラベルに含めてはならない (原則 V)", forbidden)
			}
		}
	})

	t.Run("トークンが出力に現れない", func(t *testing.T) {
		// SC-006: ログ・メトリクス・エラーメッセージのいずれにも
		// トークンが現れないこと。**トークン自体は出力しない。**
		if strings.Contains(body, f.token) {
			t.Error("/metrics にトークンが現れる (SC-006)")
		}
		if logs := f.logs.String(); strings.Contains(logs, f.token) {
			t.Error("ログにトークンが現れる (SC-006、FR-023)")
		}
	})
}
