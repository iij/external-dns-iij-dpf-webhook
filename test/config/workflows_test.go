// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"strings"
	"testing"
)

// ワークフローの不変条件。
//
// **この 2 つが本機能で最も重要な検査である。** 将来「便利だから」と自動マージを
// 足す変更や、「失敗が邪魔だから」と検査を飛ばす変更は、ここに落ちる。
// 文書の記述だけではそうした変更を止められない (plan.md「原則 III への対応」)。

// 自動マージを実現しうる記述。1 つでも現れたら失敗させる (FR-009)。
//
// pull_request_target を含めるのは、これが更新 Pull Request の文脈で
// 秘密情報つきの実行を可能にする入口だからである。自動マージそのものでは
// ないが、FR-013 (更新後の依存コードと秘密情報を同居させない) を破る経路に
// なる。既定で塞いでおく (原則 VI)。
var forbiddenAutoMergePatterns = []string{
	"gh pr merge",
	"--auto",
	"enable-pull-request-automerge",
	"pull_request_target",
	"automerge",
	"auto-merge",
}

// T022: 自動マージを行う仕組みが存在しないこと (FR-009)
//
// 憲章「開発ワークフローと品質ゲート」がレビュー承認なしのマージを禁じている。
// マージは人のレビューと承認を経る。
func TestNoAutoMergeMechanism(t *testing.T) {
	workflows, err := loadWorkflows()
	if err != nil {
		t.Fatalf("ワークフローを読めなかった: %v", err)
	}

	targets := make(map[string]string, len(workflows)+1)
	for _, w := range workflows {
		targets[w.Name] = w.Raw
	}

	// dependabot.yml も対象に含める。設定側から自動マージを有効にする
	// 記述が入る余地を残さない。
	raw, err := os.ReadFile(dependabotPath) //nolint:gosec // 検査対象はリポジトリ内の固定パス
	if err != nil {
		t.Fatalf("%s を読めなかった: %v", dependabotPath, err)
	}
	targets["dependabot.yml"] = string(raw)

	for name, content := range targets {
		lower := strings.ToLower(content)
		for _, pat := range forbiddenAutoMergePatterns {
			if !strings.Contains(lower, strings.ToLower(pat)) {
				continue
			}
			// コメント中の言及 (「自動マージしない」という説明) は許す。
			// 実効的な記述だけを咎める。
			if onlyInComments(content, pat) {
				continue
			}
			t.Errorf("%s: %q が現れる。マージは人のレビューと承認を経る (FR-009)", name, pat)
		}
	}
}

// onlyInComments は pat の出現がコメント行のみかを返す。
//
// YAML のコメントは # から行末まで。契約や理由を書いたコメントで検査が
// 落ちると、**説明を書けなくなる**。説明の無い設定の方が危険であるため、
// コメント中の言及は通す。
func onlyInComments(content, pat string) bool {
	lowerPat := strings.ToLower(pat)

	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(strings.ToLower(line), lowerPat) {
			continue
		}
		code, _, found := strings.Cut(line, "#")
		if !found {
			return false // コメントの無い行に出現している
		}
		if strings.Contains(strings.ToLower(code), lowerPat) {
			return false // # より前 (コード側) に出現している
		}
	}

	return true
}

// 秘密情報を参照している記述。これを含むジョブ・ステップが
// 条件付きで飛ばされていないことを確かめる。
const secretReference = "secrets."

// 起点によって値が変わる式。これを if: に書くと、Dependabot 起点の実行で
// 検査が skipped になる。
var actorConditionMarkers = []string{
	"github.actor",
	"dependabot",
	"github.event.pull_request.user",
	"github.triggering_actor",
}

// T023: 秘密情報を要する検査が条件付きで飛ばされていないこと (FR-014、SC-004)
//
// **飛ばした検査は「skipped」となり、ブランチ保護では成功として数えられる。**
// 検査を経ずにマージできる状態が生まれる。秘密情報が無いなら失敗させる。
// 「実行できなかった」と「通った」を同じ色にしない。
func TestSecretDependentChecksAreNotSkipped(t *testing.T) {
	workflows, err := loadWorkflows()
	if err != nil {
		t.Fatalf("ワークフローを読めなかった: %v", err)
	}

	for _, w := range workflows {
		for jobName, job := range w.Doc.Jobs {
			jobUsesSecret := jobReferencesSecret(job)

			if jobUsesSecret && hasActorCondition(job.If) {
				t.Errorf("%s: ジョブ %s が秘密情報を参照しつつ if: %q で分岐している。"+
					"飛ばした検査は skipped となり成功として数えられる (FR-014)",
					w.Name, jobName, job.If)
			}

			for _, step := range job.Steps {
				if !stepReferencesSecret(step) {
					continue
				}
				if hasActorCondition(step.If) {
					t.Errorf("%s: ジョブ %s のステップ %q が秘密情報を参照しつつ if: %q で分岐している (FR-014)",
						w.Name, jobName, step.Name, step.If)
				}
				// ジョブ側の if で飛ばされる場合も同じ結果になる。
				if hasActorCondition(job.If) {
					t.Errorf("%s: ジョブ %s のステップ %q が秘密情報を参照するが、ジョブが if: %q で分岐している (FR-014)",
						w.Name, jobName, step.Name, job.If)
				}
			}
		}
	}
}

func jobReferencesSecret(job workflowJob) bool {
	for _, step := range job.Steps {
		if stepReferencesSecret(step) {
			return true
		}
	}

	return false
}

func stepReferencesSecret(step workflowStep) bool {
	for _, v := range step.Env {
		if s, ok := v.(string); ok && strings.Contains(s, secretReference) {
			return true
		}
	}
	for _, v := range step.With {
		if s, ok := v.(string); ok && strings.Contains(s, secretReference) {
			return true
		}
	}

	return strings.Contains(step.Run, secretReference)
}

func hasActorCondition(cond string) bool {
	if cond == "" {
		return false
	}
	lower := strings.ToLower(cond)
	for _, m := range actorConditionMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}

	return false
}
