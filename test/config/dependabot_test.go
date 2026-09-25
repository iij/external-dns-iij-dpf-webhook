// SPDX-License-Identifier: Apache-2.0

package config

import (
	"slices"
	"strings"
	"testing"
)

// .github/dependabot.yml の不変条件。
//
// contracts/dependabot-config.md の MUST / MUST NOT を機械的に固定する。
// 文書の記述だけでは、後から緩める変更を止められない。

// 期待する 3 種別とその対象ディレクトリ (FR-004、research「対象となる依存の所在」)。
var wantEcosystems = map[string]string{
	"gomod":          "/",
	"github-actions": "/",
	"docker":         "/build",
}

// Dependabot の既定値。設定を省略するとここに緩む。
const (
	defaultCooldownDays      = 3
	defaultOpenPRLimit       = 5
	requiredCooldownDays     = 5
	requiredScheduleInterval = "weekly"
	requiredLabel            = "dependencies"
)

func mustLoadDependabot(t *testing.T) *dependabotConfig {
	t.Helper()

	cfg, err := loadDependabot()
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", dependabotPath, err)
	}
	if len(cfg.Updates) == 0 {
		t.Fatalf("%s の updates が空である。検査する対象が無い", dependabotPath)
	}

	return cfg
}

// T007: 待機期間 (FR-002、FR-003)
//
// **省略を失敗として扱う。** 省略すると Dependabot の既定 3 日に緩み、
// 「5 日」という要件と静かに食い違う。設定が書かれていないことは、
// 誤った値が書かれていることと同じ重さで扱う。
func TestCooldownIsFiveDays(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		if u.Cooldown == nil {
			t.Errorf("%s: cooldown が無い。省略すると既定 %d 日に緩む (FR-002)",
				u.PackageEcosystem, defaultCooldownDays)
			continue
		}
		if u.Cooldown.DefaultDays == nil {
			t.Errorf("%s: cooldown.default-days が無い。省略すると既定 %d 日に緩む (FR-002)",
				u.PackageEcosystem, defaultCooldownDays)
			continue
		}
		if got := *u.Cooldown.DefaultDays; got != requiredCooldownDays {
			t.Errorf("%s: cooldown.default-days = %d、%d であること (FR-002)",
				u.PackageEcosystem, got, requiredCooldownDays)
		}
	}
}

// T007 (続き): 待機期間をセキュリティ更新へ広げないこと (FR-003)
//
// Dependabot はバージョン更新にのみ待機期間を適用する。この既定を変える
// 記述を置かない。修正の適用が遅れることの害が、待機で得られる安全性を上回る。
//
// semver-*-days も書かない (research R1: github-actions と docker が対応せず、
// 種別ごとに意味の違う設定になる)。
func TestCooldownIsNotBroadened(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		if u.Cooldown == nil {
			continue // TestCooldownIsFiveDays が報告する
		}
		if len(u.Cooldown.Include) > 0 || len(u.Cooldown.Exclude) > 0 {
			t.Errorf("%s: cooldown に include/exclude がある。待機期間の適用範囲を広げない (FR-003)",
				u.PackageEcosystem)
		}
		if u.Cooldown.SemverMajorDays != nil || u.Cooldown.SemverMinorDays != nil || u.Cooldown.SemverPatchDays != nil {
			t.Errorf("%s: cooldown に semver-*-days がある。3 種別で書き方を揃えられないため一律 default-days とする (research R1)",
				u.PackageEcosystem)
		}
	}
}

// T008: 3 種別が揃っていること (FR-004)
func TestAllThreeEcosystemsArePresent(t *testing.T) {
	cfg := mustLoadDependabot(t)

	seen := make(map[string]string, len(cfg.Updates))
	for _, u := range cfg.Updates {
		if _, dup := seen[u.PackageEcosystem]; dup {
			t.Errorf("%s の updates 要素が 2 つある。種別ごとに 1 つであること", u.PackageEcosystem)
		}
		seen[u.PackageEcosystem] = u.Directory
	}

	for eco, wantDir := range wantEcosystems {
		gotDir, ok := seen[eco]
		if !ok {
			t.Errorf("種別 %s が無い。この種別の更新は 1 件も提案されない (FR-004)", eco)
			continue
		}
		if gotDir != wantDir {
			t.Errorf("%s: directory = %q、%q であること (FR-004)", eco, gotDir, wantDir)
		}
	}

	for eco := range seen {
		if _, want := wantEcosystems[eco]; !want {
			t.Errorf("想定していない種別 %s がある。対象を増やすなら spec と contract を先に更新する", eco)
		}
	}
}

// T009: 種別ごとにまとめられること (FR-005)
//
// まとめないと、検証用ゾーンを共有する検査が提案の数だけ直列に積み上がる
// (research R6)。
func TestUpdatesAreGrouped(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		if len(u.Groups) == 0 {
			t.Errorf("%s: groups が無い。依存 1 件ごとに Pull Request が作られる (FR-005)", u.PackageEcosystem)
			continue
		}
		if len(u.Groups) != 1 {
			t.Errorf("%s: groups が %d 個ある。種別内は 1 つにまとめる (FR-005)",
				u.PackageEcosystem, len(u.Groups))
		}
		for name, g := range u.Groups {
			if !slices.Equal(g.Patterns, []string{"*"}) {
				t.Errorf("%s: groups.%s.patterns = %v、[\"*\"] であること。種別内の全依存をまとめる (FR-005)",
					u.PackageEcosystem, name, g.Patterns)
			}
			if len(g.ExcludePatterns) > 0 {
				t.Errorf("%s: groups.%s に exclude-patterns がある。除外した依存は個別の Pull Request になる (FR-005)",
					u.PackageEcosystem, name)
			}
		}
	}
}

// T010: 提案数の上限 (FR-008)
//
// **省略を失敗として扱う。** 省略すると既定の 5 になる。既定より狭くするのは、
// 検証の待ち行列を有界に保つためである (原則 VI)。
func TestOpenPullRequestsLimitIsNarrowerThanDefault(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		if u.OpenPullRequestsLimit == nil {
			t.Errorf("%s: open-pull-requests-limit が無い。省略すると既定 %d に緩む (FR-008)",
				u.PackageEcosystem, defaultOpenPRLimit)
			continue
		}
		got := *u.OpenPullRequestsLimit
		if got >= defaultOpenPRLimit {
			t.Errorf("%s: open-pull-requests-limit = %d、既定 %d より小さいこと。検証の待ち行列を有界に保つ (FR-008)",
				u.PackageEcosystem, got, defaultOpenPRLimit)
		}
		if got <= 0 {
			t.Errorf("%s: open-pull-requests-limit = %d。0 以下は提案を止めてしまう", u.PackageEcosystem, got)
		}
	}
}

// T011: メジャー版を除外しないこと (FR-007)
//
// 提案は行い、取り込むかどうかは人が判断する。除外すると互換性のない更新が
// 視界から消え、いつまでも古い版に留まっていることに気付けない。
func TestMajorVersionsAreNotIgnored(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		for _, ig := range u.Ignore {
			for _, ut := range ig.UpdateTypes {
				if strings.Contains(ut, "semver-major") {
					t.Errorf("%s: ignore に %q がある。メジャー版も提案の対象とする (FR-007)",
						u.PackageEcosystem, ut)
				}
			}
		}
		for _, g := range u.Groups {
			for _, ut := range g.UpdateTypes {
				if strings.Contains(ut, "semver-major") {
					t.Errorf("%s: groups の update-types が %q に限定されている。メジャー版を落とさない (FR-007)",
						u.PackageEcosystem, ut)
				}
			}
		}
	}
}

// T012: registries を置かないこと
//
// github.com/iij/dpf-go は 2026-09-14 に公開され、モジュールの取得に認証は
// 要らなくなった。不要になった資格情報の経路を残さない
// (contracts/dependabot-config.md「書いてはならない内容」、research R3 改訂)。
func TestNoRegistries(t *testing.T) {
	cfg := mustLoadDependabot(t)

	if len(cfg.Registries) > 0 {
		names := make([]string, 0, len(cfg.Registries))
		for n := range cfg.Registries {
			names = append(names, n)
		}
		slices.Sort(names)
		t.Errorf("registries がある (%v)。dpf-go は公開済みでモジュール取得に認証は要らない", names)
	}

	for _, u := range cfg.Updates {
		if strings.Contains(u.PackageEcosystem, "registries") {
			t.Errorf("%s: registries を参照している", u.PackageEcosystem)
		}
	}
}

// T013: 検出の間隔とラベル
//
// 週 1 回とするのは、待機期間 5 日と組み合わせたとき提案がまとまるためである
// (spec Assumptions)。ラベルは滞留の検査が Pull Request を見分ける手掛かりになる。
func TestScheduleAndLabels(t *testing.T) {
	cfg := mustLoadDependabot(t)

	for _, u := range cfg.Updates {
		if u.Schedule.Interval != requiredScheduleInterval {
			t.Errorf("%s: schedule.interval = %q、%q であること (spec Assumptions)",
				u.PackageEcosystem, u.Schedule.Interval, requiredScheduleInterval)
		}
		if !slices.Contains(u.Labels, requiredLabel) {
			t.Errorf("%s: labels に %q が無い (%v)。滞留の検査が対象を見分けられない (FR-018)",
				u.PackageEcosystem, requiredLabel, u.Labels)
		}
	}
}

// 設定の版。version: 2 以外は Dependabot が解釈しない。
func TestConfigVersionIsTwo(t *testing.T) {
	cfg := mustLoadDependabot(t)

	if cfg.Version != 2 {
		t.Errorf("version = %d、2 であること", cfg.Version)
	}
}
