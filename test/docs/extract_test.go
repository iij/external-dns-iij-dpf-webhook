// SPDX-License-Identifier: Apache-2.0

package docs

import (
	"errors"
	"slices"
	"testing"
)

// 印の直後の表から行を取り出す。
//
// 見出し行と区切り行は行に含めない。セルからは装飾を剥がす。文書は名前を
// `--domain-filter` のように書くが、実装側の値は装飾を持たない。この差を
// 検査が吸収する。
func TestTable_ReadsRowsAfterMarker(t *testing.T) {
	t.Parallel()

	doc := "" +
		"# 見出し\n" +
		"\n" +
		"本文。ここには表がない。\n" +
		"\n" +
		"<!-- reference:flags -->\n" +
		"\n" +
		"| フラグ | 既定 | 内容 |\n" +
		"|---|---|---|\n" +
		"| `--domain-filter` | (なし) | 管理対象ドメイン |\n" +
		"| **`--log-level`** | info | ログレベル |\n" +
		"\n" +
		"次の段落。\n"

	rows, err := table(doc, "flags")
	if err != nil {
		t.Fatalf("table = error %v", err)
	}

	want := [][]string{
		{"--domain-filter", "(なし)", "管理対象ドメイン"},
		{"--log-level", "info", "ログレベル"},
	}
	if len(rows) != len(want) {
		t.Fatalf("行数 = %d, want %d: %v", len(rows), len(want), rows)
	}
	for i := range want {
		if !slices.Equal(rows[i], want[i]) {
			t.Errorf("%d 行目 = %v, want %v", i, rows[i], want[i])
		}
	}
}

// 印が複数あっても、指定した名前の表だけを読む。
func TestTable_PicksTheNamedMarker(t *testing.T) {
	t.Parallel()

	doc := "" +
		"<!-- reference:flags -->\n" +
		"| a | b |\n" +
		"|---|---|\n" +
		"| 1 | 2 |\n" +
		"\n" +
		"<!-- reference:metrics -->\n" +
		"| 名前 |\n" +
		"|---|\n" +
		"| `dns_record_changes_total` |\n"

	rows, err := table(doc, "metrics")
	if err != nil {
		t.Fatalf("table = error %v", err)
	}
	if len(rows) != 1 || rows[0][0] != "dns_record_changes_total" {
		t.Errorf("rows = %v, want [[dns_record_changes_total]]", rows)
	}
}

// 文書中のすべての印の名前を返す。未知の名前を検出するために使う。
func TestMarkers_ListsEveryName(t *testing.T) {
	t.Parallel()

	doc := "" +
		"<!-- reference:flags -->\n| a |\n|---|\n| 1 |\n\n" +
		"<!-- reference:metrics -->\n| a |\n|---|\n| 1 |\n"

	got := markers(doc)
	want := []string{"flags", "metrics"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("markers = %v, want %v", got, want)
	}
}

// 印にまつわる異常はすべてエラーとして返す。
//
// **黙って空を返さない。** 空を返すと、印を消すだけで検査を無効化でき、
// 表ごと消しても気付けない (原則 VI)。
func TestTable_RejectsAnomalies(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  string
		want error
	}{
		{
			name: "印がない",
			doc:  "| a |\n|---|\n| 1 |\n",
			want: errMarkerNotFound,
		},
		{
			name: "同じ印が 2 つ",
			doc: "<!-- reference:flags -->\n| a |\n|---|\n| 1 |\n\n" +
				"<!-- reference:flags -->\n| a |\n|---|\n| 2 |\n",
			want: errMarkerDuplicated,
		},
		{
			name: "印の直後に表がない",
			doc:  "<!-- reference:flags -->\n\n本文だけ。\n",
			want: errNoTable,
		},
		{
			name: "表が見出し行だけ",
			doc:  "<!-- reference:flags -->\n| a | b |\n|---|---|\n\n次の段落。\n",
			want: errEmptyTable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := table(tc.doc, "flags")
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// 構文木の読み取りは、呼び出しの引数である文字列リテラルだけを拾う。
//
// 文字列検索と違い、コメントや無関係な変数の値を拾わない。ここが崩れると
// 検査が誤検知を出し、やがて誰も信用しなくなる。
func TestCallStringArgs_TakesOnlyCallArguments(t *testing.T) {
	t.Parallel()

	got, err := callStringArgs("testdata/sample", "observe", 0)
	if err != nil {
		t.Fatalf("callStringArgs = error %v", err)
	}

	want := []string{"first", "second"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got = %v, want %v", got, want)
	}
}

// テストファイルは対象から外す。検査用の題材を実装の事実と混ぜない。
func TestCallStringArgs_SkipsTestFiles(t *testing.T) {
	t.Parallel()

	// 本パッケージ自身を読む。_test.go にしか現れない文字列を拾わないこと。
	got, err := callStringArgs(".", "table", 1)
	if err != nil {
		t.Fatalf("callStringArgs = error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want 空 (table の呼び出しは _test.go にしかない)", got)
	}
}

// 変数に与えられた文字列リテラルを集める。
func TestSliceVarStrings(t *testing.T) {
	t.Parallel()

	got, err := sliceVarStrings("testdata/sample", "supported")
	if err != nil {
		t.Fatalf("sliceVarStrings = error %v", err)
	}

	want := []string{"alpha", "beta"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("got = %v, want %v", got, want)
	}
}

// 存在しない変数名では空を返す。呼び出し側が「見つからない」を検出できること。
func TestSliceVarStrings_Missing(t *testing.T) {
	t.Parallel()

	got, err := sliceVarStrings("testdata/sample", "存在しない")
	if err != nil {
		t.Fatalf("sliceVarStrings = error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got = %v, want 空", got)
	}
}
