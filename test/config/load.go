// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// 検査はリポジトリの根にあるファイルを見る。テストの作業ディレクトリは
// パッケージのディレクトリになるため、そこからの相対で根を指す。
const repoRoot = "../.."

var (
	dependabotPath = filepath.Join(repoRoot, ".github", "dependabot.yml")
	workflowsDir   = filepath.Join(repoRoot, ".github", "workflows")
)

// dependabotConfig は .github/dependabot.yml の構造のうち、本機能が
// 要件として固定する部分を保持する。
//
// 省略と零値を区別する必要がある項目はポインタで持つ。cooldown と
// open-pull-requests-limit がそれで、**省略されていること自体が要件違反**
// である (省略すると Dependabot の既定に緩む)。値で受けると省略と 0 が
// 同じに見え、検査が素通しになる。
type dependabotConfig struct {
	Version    int                     `yaml:"version"`
	Registries map[string]any          `yaml:"registries"`
	Updates    []dependabotUpdateEntry `yaml:"updates"`
}

type dependabotUpdateEntry struct {
	PackageEcosystem      string                     `yaml:"package-ecosystem"`
	Directory             string                     `yaml:"directory"`
	Schedule              dependabotSchedule         `yaml:"schedule"`
	Cooldown              *dependabotCooldown        `yaml:"cooldown"`
	Groups                map[string]dependabotGroup `yaml:"groups"`
	OpenPullRequestsLimit *int                       `yaml:"open-pull-requests-limit"`
	Labels                []string                   `yaml:"labels"`
	Ignore                []dependabotIgnore         `yaml:"ignore"`
}

type dependabotSchedule struct {
	Interval string `yaml:"interval"`
	Day      string `yaml:"day"`
	Time     string `yaml:"time"`
	Timezone string `yaml:"timezone"`
}

// dependabotCooldown は待機期間。SemverMajorDays 以下を保持するのは、
// **それらが書かれていないこと**を検査するためである (research R1:
// 3 種別で書き方を揃えられないため一律 default-days とする)。
type dependabotCooldown struct {
	DefaultDays     *int     `yaml:"default-days"`
	SemverMajorDays *int     `yaml:"semver-major-days"`
	SemverMinorDays *int     `yaml:"semver-minor-days"`
	SemverPatchDays *int     `yaml:"semver-patch-days"`
	Include         []string `yaml:"include"`
	Exclude         []string `yaml:"exclude"`
}

type dependabotGroup struct {
	Patterns        []string `yaml:"patterns"`
	ExcludePatterns []string `yaml:"exclude-patterns"`
	UpdateTypes     []string `yaml:"update-types"`
}

type dependabotIgnore struct {
	DependencyName string   `yaml:"dependency-name"`
	Versions       []string `yaml:"versions"`
	UpdateTypes    []string `yaml:"update-types"`
}

// loadDependabot はリポジトリの .github/dependabot.yml を読む。
func loadDependabot() (*dependabotConfig, error) {
	return loadDependabotFrom(dependabotPath)
}

// loadDependabotFrom は指定した path の設定を読む。
//
// **ファイルが無い場合はエラーを返す。** 「無いので何も検査しない」で
// 通すと、設定を消すだけで不変条件の検査をすべて無効化できる (原則 VI)。
func loadDependabotFrom(path string) (*dependabotConfig, error) {
	// nolint の理由: path はリポジトリ内の固定パスか、テストが用意した一時
	// ディレクトリのみ。外部入力は入らない (.golangci.yml の方針に従い理由を併記)。
	raw, err := os.ReadFile(path) //nolint:gosec // 検査対象はリポジトリ内の固定パス
	if err != nil {
		return nil, fmt.Errorf("設定を読めない (%s): %w", path, err)
	}

	var cfg dependabotConfig
	// KnownFields で未知のキーを拒む。綴りの誤りが静かに無視されると、
	// 書いたつもりの設定が効いていない状態に気付けない。
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("設定を解析できない (%s): %w", path, err)
	}

	return &cfg, nil
}

// workflowFile は .github/workflows/*.yml の 1 本。
//
// Raw と Doc の両方を持つ。自動マージの検査は文字列としての出現を見る方が
// 確実であり (uses・run・with のどこに書かれても拾う)、検査飛ばしの検査は
// job と step の if を構造として見る必要があるためである。
type workflowFile struct {
	Name string
	Path string
	Raw  string
	Doc  workflowDoc
}

type workflowDoc struct {
	Name string                 `yaml:"name"`
	Jobs map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Name  string         `yaml:"name"`
	If    string         `yaml:"if"`
	Needs any            `yaml:"needs"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name string         `yaml:"name"`
	If   string         `yaml:"if"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	Env  map[string]any `yaml:"env"`
	With map[string]any `yaml:"with"`
}

// loadWorkflows はリポジトリの .github/workflows/*.yml をすべて読む。
func loadWorkflows() ([]workflowFile, error) {
	return loadWorkflowsFrom(workflowsDir)
}

// loadWorkflowsFrom は指定したディレクトリのワークフローを読む。
//
// **1 本も見つからない場合はエラーを返す。** 対象が空でも成功する検査は、
// ワークフローを消すだけで無効化できる (原則 VI)。
func loadWorkflowsFrom(dir string) ([]workflowFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("ワークフローのディレクトリを読めない (%s): %w", dir, err)
	}

	var workflows []workflowFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yml" && ext != ".yaml" {
			continue
		}

		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path) //nolint:gosec // 検査対象は .github/workflows 配下の固定パス
		if err != nil {
			return nil, fmt.Errorf("ワークフローを読めない (%s): %w", path, err)
		}

		var doc workflowDoc
		// ワークフローは本機能が定めた形ではないため KnownFields は使わない。
		// 必要な部分だけを取り出す。
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return nil, fmt.Errorf("ワークフローを解析できない (%s): %w", path, err)
		}

		workflows = append(workflows, workflowFile{
			Name: e.Name(),
			Path: path,
			Raw:  string(raw),
			Doc:  doc,
		})
	}

	if len(workflows) == 0 {
		return nil, fmt.Errorf("ワークフローが 1 本も見つからない (%s)。対象が空のまま成功させない", dir)
	}

	sort.Slice(workflows, func(i, j int) bool { return workflows[i].Name < workflows[j].Name })

	return workflows, nil
}
