#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""取り込まれずに残っている更新の提案がないかを確かめる。

マージが人の操作になったため、Pull Request が開いたまま忘れられる状態は
必ず生まれる (FR-018、SC-008)。放置された脆弱性の修正は、自動化していない
場合より危険である。「自動化してあるから大丈夫」と思われるためである
(research R5)。

**自動で閉じない (FR-011)。** 閉じても更新の必要は消えず、次の検出で同じ
提案が作られるだけである。伝えるところまでを行う。

入力:
  prs.json  `gh pr list --json number,title,createdAt,url` の出力 (JSON 配列)
"""

# 注釈を遅延評価にする。X | None (PEP 604) は Python 3.10 以降のため、
# 手元の 3.9 でも動くようにする。CI は 3.12 だが、手元で走らない検査にしない。
from __future__ import annotations

import json
import sys
from datetime import datetime, timedelta, timezone

# この日数を超えて開いたままの提案を報告する (SC-008)。
STALE_DAYS = 30


def parse_time(value: str) -> datetime | None:
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def stale(prs: list[dict], now: datetime, days: int) -> list[tuple[dict, int]]:
    threshold = now - timedelta(days=days)
    found = []

    for pr in prs:
        created = parse_time(pr.get("createdAt", ""))
        if created is None:
            continue
        if created < threshold:
            found.append((pr, (now - created).days))

    return found


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: check_stale_prs.py <prs.json>", file=sys.stderr)
        return 2

    with open(sys.argv[1], encoding="utf-8") as f:
        text = f.read()

    prs = json.loads(text) if text.strip() else []
    now = datetime.now(timezone.utc)
    found = stale(prs, now, STALE_DAYS)

    print(f"開いている更新 Pull Request: {len(prs)} 件")

    if not found:
        print(f"{STALE_DAYS} 日を超えて滞留しているものはありません。")
        return 0

    print()
    print(f"::error::{STALE_DAYS} 日を超えて滞留している更新 Pull Request が {len(found)} 件あります。")
    for pr, age in sorted(found, key=lambda x: -x[1]):
        print(f"  #{pr.get('number')} ({age} 日) {pr.get('title', '')}")
        print(f"    {pr.get('url', '')}")
    print()
    print("自動では閉じません (FR-011)。取り込むか、取り込まない理由を残してください。")

    return 1


if __name__ == "__main__":
    sys.exit(main())
