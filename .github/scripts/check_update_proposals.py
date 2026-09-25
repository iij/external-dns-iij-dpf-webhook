#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""更新が提案されていない依存がないかを確かめる。

Dependabot の更新処理が壊れている状態は、「更新がない」状態と外から区別が
つかない。**Go の依存が 1 件も提案されなくなるが、リポジトリは静かなまま**に
なる。仕様が FR-017 と SC-009 でこれを禁じている。

Dependabot の実行状態を問い合わせる公開 API はない。そこで **Dependabot に
依存しない側から見る。** 自分で更新可能な依存を数え、提案と突き合わせる。
差があれば、提案の仕組みが動いていない (research R4)。

**待機期間内の版を「提案されていない」と報告しない。** 公開から 5 日未満の
版は提案されないのが正しい (FR-002)。これを報告すると、毎回鳴る警報になり、
本当の欠落が埋もれる。

入力:
  modules.json  `go list -m -u -json all` の出力 (JSON オブジェクトの連結)
  prs.json      `gh pr list --json number,title,body` の出力 (JSON 配列)
"""

# 注釈を遅延評価にする。X | None (PEP 604) は Python 3.10 以降のため、
# 手元の 3.9 でも動くようにする。CI は 3.12 だが、手元で走らない検査にしない。
from __future__ import annotations

import json
import sys
from datetime import datetime, timedelta, timezone

# 版の公開からこの日数が経つまで提案されない (.github/dependabot.yml の
# cooldown.default-days と同じ値)。**片方だけ変えないこと。**
COOLDOWN_DAYS = 5


def iter_json_objects(text: str):
    """連結された JSON オブジェクトを 1 つずつ返す。

    go list -m -json は配列ではなくオブジェクトの連結を出す。
    """
    decoder = json.JSONDecoder()
    idx = 0
    length = len(text)
    while idx < length:
        while idx < length and text[idx].isspace():
            idx += 1
        if idx >= length:
            return
        obj, end = decoder.raw_decode(text, idx)
        yield obj
        idx = end


def parse_time(value: str) -> datetime | None:
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def expected_updates(modules_text: str, now: datetime) -> list[tuple[str, str]]:
    """待機期間を満たし、提案されているべき直接依存を返す。"""
    threshold = now - timedelta(days=COOLDOWN_DAYS)
    expected = []

    for mod in iter_json_objects(modules_text):
        if mod.get("Main") or mod.get("Indirect"):
            continue
        update = mod.get("Update")
        if not update:
            continue

        published = parse_time(update.get("Time", ""))
        if published is None:
            # 公開日が分からない版は待機期間を判定できない。**報告しない。**
            # 判定できないものを欠落として挙げると、毎回鳴る警報になる。
            continue
        if published > threshold:
            continue  # 待機期間内。提案されないのが正しい

        expected.append((mod["Path"], update["Version"]))

    return expected


def proposed_paths(prs_text: str) -> str:
    """開いている Dependabot Pull Request の題名と本文をまとめて返す。"""
    prs = json.loads(prs_text) if prs_text.strip() else []

    return "\n".join(f"{pr.get('title', '')}\n{pr.get('body', '')}" for pr in prs)


def main() -> int:
    if len(sys.argv) != 3:
        print("usage: check_update_proposals.py <modules.json> <prs.json>", file=sys.stderr)
        return 2

    with open(sys.argv[1], encoding="utf-8") as f:
        modules_text = f.read()
    with open(sys.argv[2], encoding="utf-8") as f:
        prs_text = f.read()

    now = datetime.now(timezone.utc)
    expected = expected_updates(modules_text, now)
    haystack = proposed_paths(prs_text)

    missing = [(path, version) for path, version in expected if path not in haystack]

    if not expected:
        print(f"待機期間 ({COOLDOWN_DAYS} 日) を満たす更新可能な直接依存はありません。")
        return 0

    print(f"待機期間を満たす更新可能な直接依存: {len(expected)} 件")
    for path, version in expected:
        mark = "提案なし" if (path, version) in missing else "提案あり"
        print(f"  [{mark}] {path} -> {version}")

    if not missing:
        print("すべて提案されています。")
        return 0

    print()
    print(f"::error::更新可能な直接依存 {len(missing)} 件が提案されていません。")
    print("「更新がない」のではなく、提案の仕組みが動いていない可能性があります。")
    print("Insights → Dependency graph → Dependabot でエラーを確認してください。")

    return 1


if __name__ == "__main__":
    sys.exit(main())
