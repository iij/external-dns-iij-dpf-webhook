#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0

"""生成された SBOM が配布に足る内容かを確かめる。

空や壊れた SBOM をそのまま添付すると、供給網の情報が「あるのに使えない」
状態で配布される。添付する前にここで止める。

走査対象を取り違えた場合 (ソースツリーを見てしまった、イメージが空だった等) も
ここで検出できるよう、件数と必須パッケージの両方を見る。
"""

import json
import sys

# 本サービスは 250 を超えるモジュールに依存する。極端に少ない場合、
# 走査対象を取り違えているか、カタログが機能していない。
MIN_PACKAGES = 50

# SBOM に必ず現れるパッケージ。
#
# 自身のモジュール、ドメイン名の扱いを担う依存、DPF クライアントの 3 つを見る。
# いずれかが欠けていれば、走査対象か構成が想定と違う。
REQUIRED_PACKAGES = (
    "github.com/iij/external-dns-iij-dpf-webhook",
    "github.com/miekg/dns",
    "github.com/iij/dpf-go",
)


def verify(path: str) -> list[str]:
    """検証し、問題の一覧を返す。空なら問題なし。"""
    problems: list[str] = []

    try:
        with open(path, encoding="utf-8") as f:
            doc = json.load(f)
    except json.JSONDecodeError as e:
        return [f"JSON として解釈できません: {e}"]
    except OSError as e:
        return [f"読み取れません: {e}"]

    version = doc.get("spdxVersion", "")
    if not str(version).startswith("SPDX-"):
        problems.append(f"spdxVersion が SPDX 形式ではありません: {version!r}")

    packages = doc.get("packages") or []
    if len(packages) < MIN_PACKAGES:
        problems.append(
            f"packages が {len(packages)} 件しかありません "
            f"(最低 {MIN_PACKAGES} 件)。走査対象が誤っている可能性があります"
        )

    names = {p.get("name", "") for p in packages}
    for required in REQUIRED_PACKAGES:
        if required not in names:
            problems.append(f"SBOM に {required} が含まれていません")

    if not problems:
        print(f"OK: {version} / packages {len(packages)} 件")

    return problems


def main() -> int:
    if len(sys.argv) != 2:
        print(f"使い方: {sys.argv[0]} <sbom.spdx.json>", file=sys.stderr)
        return 2

    problems = verify(sys.argv[1])
    for p in problems:
        print(f"::error::SBOM の検証に失敗: {p}", file=sys.stderr)

    return 1 if problems else 0


if __name__ == "__main__":
    sys.exit(main())
