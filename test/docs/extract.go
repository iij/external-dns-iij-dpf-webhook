// SPDX-License-Identifier: Apache-2.0

package docs

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// 印にまつわる異常。**いずれも空を返さずエラーにする。**
//
// 空を返す実装にすると、印を消すだけで検査を無効化でき、表ごと消しても
// 気付けない。原則 VI (Default-Deny) をこの検査自身にも適用する。
var (
	errMarkerNotFound   = errors.New("docs: 印が見つかりません")
	errMarkerDuplicated = errors.New("docs: 同じ名前の印が複数あります")
	errNoTable          = errors.New("docs: 印の直後に表がありません")
	errEmptyTable       = errors.New("docs: 表に行がありません")
)

// markerPattern は検査対象を指す印。単独の行に置かれる。
var markerPattern = regexp.MustCompile(`^<!--\s*reference:([a-z0-9-]+)\s*-->$`)

// markers は文書に現れるすべての印の名前を、出現順に返す。
//
// 重複はそのまま含める。呼び出し側が重複を検出できるようにするため。
func markers(doc string) []string {
	var out []string
	for _, line := range strings.Split(doc, "\n") {
		if m := markerPattern.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// table は名前の印の直後にある表から、データ行を取り出す。
//
// 見出し行と区切り行 (|---|) は含めない。印と表の間に空行があってよい。
// 表以外の行が現れた時点で、その印には表がないと判断する。
//
// セルからは装飾を剥がす。文書は名前を `--domain-filter` や **太字** で書くが、
// 実装側の値は装飾を持たない。この差はここで吸収する。剥がし方を 1 か所に
// 置くことで、対象ごとに文書の書き方が割れないようにする。
func table(doc, name string) ([][]string, error) {
	lines := strings.Split(doc, "\n")

	start := -1
	for i, line := range lines {
		m := markerPattern.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil || m[1] != name {
			continue
		}
		if start >= 0 {
			return nil, fmt.Errorf("%w: reference:%s", errMarkerDuplicated, name)
		}
		start = i
	}
	if start < 0 {
		return nil, fmt.Errorf("%w: reference:%s", errMarkerNotFound, name)
	}

	// 印の後、最初の表を探す。空行は読み飛ばすが、それ以外の行が現れたら
	// 「表がない」と判断する。離れた場所の表を誤って拾わないため。
	i := start + 1
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !isTableRow(lines[i]) {
		return nil, fmt.Errorf("%w: reference:%s", errNoTable, name)
	}

	var rows [][]string
	for ; i < len(lines) && isTableRow(lines[i]); i++ {
		if isSeparatorRow(lines[i]) {
			continue
		}
		rows = append(rows, splitRow(lines[i]))
	}

	// 先頭は見出し行。データ行はその後ろ。
	if len(rows) < 2 {
		return nil, fmt.Errorf("%w: reference:%s", errEmptyTable, name)
	}
	return rows[1:], nil
}

func isTableRow(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

// isSeparatorRow は |---|---| の形の区切り行かを報告する。
func isSeparatorRow(line string) bool {
	for _, cell := range splitRow(line) {
		if cell == "" {
			continue
		}
		if strings.Trim(cell, "-:") != "" {
			return false
		}
	}
	return true
}

// splitRow は表の 1 行をセルに分け、各セルの装飾を剥がす。
func splitRow(line string) []string {
	trimmed := strings.Trim(strings.TrimSpace(line), "|")
	parts := strings.Split(trimmed, "|")

	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, undecorate(p))
	}
	return out
}

// undecorate はセルから Markdown の装飾を取り除く。
func undecorate(cell string) string {
	s := strings.TrimSpace(cell)
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "`", "")
	return strings.TrimSpace(s)
}

// parseDir は dir 直下の Go ファイル (テストを除く) を解析して返す。
//
// parser.ParseDir と ast.Package は非推奨のため使わない。ファイルごとに
// 解析する。
func parseDir(dir string) ([]*ast.File, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("docs: %s を読めません: %w", dir, err)
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, fmt.Errorf("docs: %s の解析に失敗: %w", name, err)
		}
		files = append(files, f)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("docs: %s に Go ファイルがありません", dir)
	}
	return files, nil
}

// callStringArgs は dir 直下の Go ファイルから、名前が funcName の呼び出しに
// 渡された argIndex 番目の文字列リテラルを集める。
//
// 構文木を読むのは、他に源がない場合に限る。公開関数から取れる事実は
// そちらを使う。検査のために製品コードへ公開関数を足さないため、
// 文字列リテラルとしてしか存在しない事実はこの手段で拾う。
//
// **呼び出しの引数だけを見る。** 文字列検索と違い、コメントや無関係な
// 文字列を拾わない。テストファイルは対象から外す。
func callStringArgs(dir, funcName string, argIndex int) ([]string, error) {
	files, err := parseDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || calleeName(call.Fun) != funcName || len(call.Args) <= argIndex {
				return true
			}
			if v, ok := stringLit(call.Args[argIndex]); ok {
				out = append(out, v)
			}
			return true
		})
	}
	return out, nil
}

// sliceVarStrings は dir 直下の Go ファイルから、名前が varName の変数に
// 与えられた文字列リテラルを集める。
//
//	var supportedSecretManagers = []string{"vault", "aws", ...}
func sliceVarStrings(dir, varName string) ([]string, error) {
	files, err := parseDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if name.Name != varName || i >= len(spec.Values) {
					continue
				}
				lit, ok := spec.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range lit.Elts {
					if v, ok := stringLit(elt); ok {
						out = append(out, v)
					}
				}
			}
			return true
		})
	}
	return out, nil
}

// calleeName は呼び出し先の名前を返す。f(...) と x.f(...) の双方を扱う。
func calleeName(fun ast.Expr) string {
	switch v := fun.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	default:
		return ""
	}
}

// stringLit は式が文字列リテラルであればその値を返す。
//
// 型変換で包まれている場合 (RecordType("A") など) は中身を見る。
func stringLit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(v.Value)
		if err != nil {
			return "", false
		}
		return s, true
	case *ast.CallExpr:
		if len(v.Args) == 1 {
			return stringLit(v.Args[0])
		}
	}
	return "", false
}

// calleeNames は dir 直下の Go ファイルから、pattern に一致する呼び出し先の
// 名前を集める。
//
// 「その種類の呼び出しがどれだけあるか」を知るために使う。未知の種類が
// 増えたことを検出できるようにするため。
func calleeNames(dir string, pattern *regexp.Regexp) ([]string, error) {
	files, err := parseDir(dir)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name := calleeName(call.Fun); pattern.MatchString(name) {
				out = append(out, name)
			}
			return true
		})
	}
	return out, nil
}
