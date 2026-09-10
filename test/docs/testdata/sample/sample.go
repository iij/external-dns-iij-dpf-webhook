// SPDX-License-Identifier: Apache-2.0

// Package sample は構文木の読み取りを検査するための題材である。
// このコメントには observe("コメント内の文字列") のような記述を含めてある。
package sample

var supported = []string{"alpha", "beta"}

var other = []string{"無関係"}

func observe(_ string, _ int) {}

func run() {
	// 呼び出しの引数だけが拾われること。
	observe("first", 1)
	observe("second", 2)

	// 引数でない文字列は拾われないこと。
	s := "拾われない"
	_ = s

	// 名前の違う呼び出しは拾われないこと。
	notObserve("拾われない")
}

func notObserve(_ string) {}
