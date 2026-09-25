// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 読み取りの土台に対する表明。
//
// 検査そのものではなく、検査が使う読み取りが正しいことを確かめる。
// ここが黙って空を返すと、上に載る不変条件の検査がすべて「対象が無いので
// 成功」になる。最も危険な壊れ方であるため、**対象が空のときエラーになること**
// を中心に据えている (原則 VI)。

func TestLoadDependabot(t *testing.T) {
	cfg, err := loadDependabot()
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", dependabotPath, err)
	}

	if cfg.Version != 2 {
		t.Errorf("version = %d、2 であること", cfg.Version)
	}
	if len(cfg.Updates) == 0 {
		t.Error("updates が空である。1 つ以上の更新設定が要る")
	}
}

func TestLoadDependabotMissingFileIsError(t *testing.T) {
	// 「ファイルが無いので何も検査しない」で通すと、設定を消すだけで
	// 不変条件の検査をすべて無効化できてしまう。
	missing := filepath.Join(t.TempDir(), "dependabot.yml")

	if _, err := loadDependabotFrom(missing); err == nil {
		t.Fatal("ファイルが無いのにエラーにならなかった")
	}
}

func TestLoadDependabotBrokenFileIsError(t *testing.T) {
	broken := filepath.Join("testdata", "broken.yml")

	if _, err := loadDependabotFrom(broken); err == nil {
		t.Fatal("解析できない内容なのにエラーにならなかった")
	}
}

func TestLoadWorkflows(t *testing.T) {
	workflows, err := loadWorkflows()
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", workflowsDir, err)
	}

	// 既存の 5 本。減ったことに気付けるよう名前で確かめる。
	want := []string{"ci.yml", "e2e-sidecar.yml", "e2e.yml", "release.yml", "scheduled.yml"}
	got := make(map[string]bool, len(workflows))
	for _, w := range workflows {
		got[w.Name] = true
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("%s が読み取れていない", name)
		}
	}

	for _, w := range workflows {
		if strings.TrimSpace(w.Raw) == "" {
			t.Errorf("%s の中身が空である", w.Name)
		}
		if len(w.Doc.Jobs) == 0 {
			t.Errorf("%s の jobs を解析できていない", w.Name)
		}
	}
}

func TestLoadWorkflowsEmptyDirIsError(t *testing.T) {
	// ワークフローが 1 本も無い状態で「成功」を返すと、
	// 自動マージの検査も検査飛ばしの検査も素通しになる。
	empty := t.TempDir()

	if _, err := loadWorkflowsFrom(empty); err == nil {
		t.Fatal("対象が 1 本も無いのにエラーにならなかった")
	}
}

func TestLoadWorkflowsMissingDirIsError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")

	if _, err := loadWorkflowsFrom(missing); err == nil {
		t.Fatal("ディレクトリが無いのにエラーにならなかった")
	}
}

// repoRoot が実際にリポジトリの根を指していることを確かめる。
// ここがずれると、以降の検査がすべて別のファイルを見ることになる。
func TestRepoRootPointsAtRepository(t *testing.T) {
	for _, marker := range []string{"go.mod", "Makefile", ".github"} {
		if _, err := os.Stat(filepath.Join(repoRoot, marker)); err != nil {
			t.Errorf("repoRoot(%s) に %s が無い: %v", repoRoot, marker, err)
		}
	}
}
