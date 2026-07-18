# Edit の差分表示 — 設計

日付: 2026-07-18
対象: TODO.md「Edit の変更内容が見えない」

## 背景と目的

Edit ツールの描画が結果メッセージ (「updated successfully」) のみで、何をどう変えたのかが失われている。
結果ではなく入力 (`old_string` → `new_string`) を擬似 unified diff として要約表示し、編集の多いセッションでも変更内容を追えるようにする。

## スコープ

- 対象ツールは **Edit のみ**。Write / NotebookEdit / MultiEdit は対象外 (MultiEdit は `edits` 配列で input の形が異なり、単純な old→new の並置では表現できないため。出現時は従来どおり生 JSON 表示)。
- 同一ファイルへの連続 Edit を「Edit ×N」と折り畳む案は**今回は見送り** (イベント列の構造変更を伴うため別件)。
- diff は真の差分計算 (Myers/LCS) を行わず、old 全行に `-`、new 全行に `+` を付けて並べる**擬似 unified diff** とする。依存ゼロを維持する。

## データモデルと parse 層

`internal/parse/model.go` の `ToolCall` にフィールドを1つ追加する:

```go
type ToolCall struct {
    // ...既存フィールド...
    Diff string // Edit のとき old→new の擬似 unified diff (他ツールは空)
}
```

`Diff != ""` をテンプレートの表示条件とし、専用の bool フラグは持たない。

`internal/parse/diff.go` (新規) に構築関数を置く:

```go
// editDiff は Edit の input から擬似 unified diff を組み立てる。
// old_string の先頭 diffMaxLines 行を "- "、new_string の先頭 diffMaxLines 行を "+ " で並べ、
// 超過分は「… (あと N 行)」の1行に畳む。old/new とも取れなければ空を返す。
func editDiff(input json.RawMessage) string
```

- 省略の閾値は定数 `diffMaxLines = 5` (片側あたり)。
- 行分割の定義: 末尾の改行を1つ落としてから `strings.Split(s, "\n")` する。落とした結果が空文字列なら0行 (その側は表示なし)。したがって `""` と `"\n"` は0行、`"\n\n"` は空行2行 (`- ` が2行) となり、空行のみの変更も行として表示する。
- 改行コードは LF のみを想定し、CRLF の正規化はしない。CRLF 入力では各行末に `\r` が残るが、HTML では不可視・JS の色分けは行頭判定のため壊れず、実害は省略行カウントのずれ程度で許容する。
- `replace_all: true` のときは先頭に `(replace_all)` の1行を添える (置換箇所数は input からは分からないため印のみ)。
- ツール呼び出しの組み立て箇所で、ツール名が `Edit` のときだけ `Diff` を設定する。既存の `Summary` (ファイルパス) は変更しない。

出力イメージ:

```
- case "Read", "Edit", "Write":
-     return s
… (あと 12 行)
+ case "Read", "Edit", "Write", "NotebookEdit":
+     return summarize(s)
… (あと 12 行)
```

## テンプレート

### HTML (`templates/default.html.tmpl`)

- ツール詳細の `<pre class="input">` の前に `{{if $e.Tool.Diff}}<pre class="diff">…</pre>{{end}}` を追加する。生 JSON の `Input` 表示は従来どおり残す。
- 色分けはテンプレート末尾の既存 JS「結果のライト整形」に乗る: 対象セレクタを `"pre.result"` から `"pre.result, pre.diff"` へ広げ、CSS の `.add` / `.del` / `.err` ルールのセレクタも `pre.diff` に効くよう拡張する (`details.tool pre.result .add` → `details.tool pre .add` の形)。テンプレート関数は増やさない。

### Markdown (`templates/default.md.tmpl`)

- `tool_call` 分岐で `{{if .Tool.Diff}}` のとき ` ```diff ` フェンスで Diff を出力する。GitHub 等のビューアで自動的に赤緑に色づく。
- 挿入位置は Result のフェンスより**前** (HTML と同じく「変更内容 → 結果」の順)。

## エッジケース

- `old_string` / `new_string` が両方欠落・両方空 → `Diff` は空 (表示なし、従来動作)。
- input JSON が壊れている → `editDiff` は黙って空を返す (描画は既存の生 JSON 表示が担保)。
- 片側だけ空 (追加のみ / 削除のみ) → 空でない側だけ `+` または `-` で表示する。
- 行数がちょうど5行 → 省略行を出さない。6行以上で「… (あと N 行)」。
- 末尾改行 → `strings.Split` の前に末尾の改行を1つだけ落とし、空行が水増しカウントされないようにする。
- 中身に `-` / `+` 始まりの行が含まれる → プレフィックスを機械的に付けるだけなので破綻しない (JS の色分けは行頭判定のため正しく効く)。

## テスト

- `internal/parse/diff_test.go` (新規): `editDiff` の単体テスト — 通常ケース、省略発生、片側空、壊れた JSON、`replace_all`、末尾改行、空文字列/`"\n"`/`"\n\n"` の行分割境界値。
- `internal/parse/toolcall_test.go`: Edit の tool_use から `Diff` が設定されることを確認する。
- golden テスト: `fixtureSession()` (markdown_test.go) は parse を通らない手書きの Session リテラルのため、**再生成だけでは `Diff` が空のまま**でテンプレート追加が検証されない。fixture の Edit ToolCall に `Diff` を設定した上で `golden.html` / `golden.md` を再生成する。golden.html は末尾空白を含むのが正であり、エディタ等の whitespace 除去で壊さないこと。
