# スキル発動エントリの特別描画 — 設計

- 日付: 2026-07-12
- 対象: cctx (Claude Code トランスクリプト整形ツール)
- ステータス: 設計承認済み・レビュー指摘反映済み (実装計画は別途)
- 改訂 (2026-07-14): html のパス表示を `file://` リンクからテキスト表示 + コピーボタンへ変更。
  スキルパスはディレクトリを指すためリンクで開いても実用にならず、`fileURL` 関数ごと削除した

## 背景と目的

スキル発動後、トランスクリプトにはスキル本文が user エントリとして記録される (本文は `Base directory for this skill: <絶対パス>` で始まる)。
現状の cctx はこれをそのまま 👤 User の発言として描画してしまい、実際のユーザー発話と区別がつかない。
また `/ohayou` のようなスラッシュコマンド行はノイズ除去で消えるため、ユーザーがスキルを呼び出した事実自体が出力に残らない。

これを、発動元に応じた専用UIとして描画する。

## 発動元の判別 (調査結果)

スキル展開エントリはどちらも `type: "user"` かつ `isMeta: true` の user エントリだが、フィールド構成が異なる。

| 発動元 | 手がかり |
|---|---|
| ユーザー呼び出し (スラッシュコマンド) | `sourceToolUseID` を持たない。`parentUuid` が `<command-name>` / `<command-args>` を含む通常 user エントリの `uuid` を直接指す (実トランスクリプトで確認済み) |
| エージェント発動 (Skill ツール) | `sourceToolUseID` を持ち、assistant の `tool_use (name: "Skill")` を指す。スキル名は `input.skill` |

## 要件

- エージェント発動: 「スキル発動」チップを発言UIの外に描画する
- ユーザー呼び出し: ユーザー発言UIに「/skill-name 引数」とスキル発動チップを埋め込んで描画する
- チップの形式: `🔧 Skill(skill-name) <絶対パス>`。パスは html ではテキスト表示 + コピーボタン、md ではインラインコード表記
- 展開本文 (スキルの手順書) はどちらの形式でも丸ごと非表示 (折りたたみにも残さない)

## データモデル (internal/parse/model.go)

```go
const KindSkillInvocation EventKind = "skill_invocation"

type SkillInvocation struct {
    Name    string // スキル名。エージェント発動は input.skill、ユーザー呼び出しは <command-name> の先頭 "/" を除いた形
    Path    string // "Base directory for this skill:" の絶対パス
    ByUser  bool   // true ならユーザー呼び出し
    Command string // ByUser のとき「/name 引数」の再現文字列 (引数込み)。エージェント発動では空
}
```

- `Event` に `Skill *SkillInvocation` を追加 (Kind が SkillInvocation のとき非 nil)
- `Stats` に `SkillInvocations int` を追加

## parse 層の検出ロジック

`rawRecord` に以下を追加パースする。

- `IsMeta bool` (`json:"isMeta"`)
- `SourceToolUseID string` (`json:"sourceToolUseID"`)
- `ParentUUID string` (`json:"parentUuid"`)

### スキル展開エントリの判定

`type: "user"` かつ `isMeta: true` かつ最初の text ブロックが `Base directory for this skill: ` で始まること。
パスはその行の残り(行末まで) から取得し、行末の `\r` と前後の空白を除去する。
合致した展開エントリは `user_message` として出力しない。
それ以外の isMeta エントリの扱いは現状のまま変えない (スコープ外)。

### エージェント発動 (sourceToolUseID あり)

`collectToolResults` と同様の事前パスで、展開エントリを `sourceToolUseID` で索引化する。
`Skill` の tool_use をイベント化する際、対応する展開があれば `tool_call` の代わりに `skill_invocation` を出す
(Name は `input.skill`、Path は展開エントリから)。
チップは Skill 呼び出しの位置に出る。

縮退:

- 対応する展開が無い場合 (権限拒否された場合など) は従来どおり `tool_call` / `permission_deny` のまま。
  permission_deny 判定の優先順は現状を維持する
- `input.skill` が空の場合、Name はパスの basename で代替する。空でない場合は basename と異なっていても常に `input.skill` を優先する
- 展開エントリが `sourceToolUseID` を持つのに対応する Skill tool_use が本流に見つからない場合、その展開エントリは描画しない
  (展開本文は常に非表示という方針に従う)

### ユーザー呼び出し (sourceToolUseID なし)

展開エントリの `parentUuid` は、対応するコマンドエントリの `uuid` を直接指す (実トランスクリプトで確認済み)。
事前パスで `<command-name>` を含む通常 user エントリを `uuid` で索引化しておき、
展開エントリの位置で `parentUuid` を引いて `skill_invocation` (ByUser=true) を出す。
「直近のコマンドを覚えておく」近接方式は採らない (欠損・変則トランスクリプトで誤った対応付けが起きるため)。

- `Command` は「/name 引数」を再現する (引数が空なら「/name」のみ)
- `<command-name>` は `/ohayou` のように先頭 `/` 付きで記録される。
  `Name` は先頭の `/` を1つだけ除いた形とする (それ以外の `/` や `:` はそのまま保持する)
- `<command-args>` が空白のみの場合は引数なしとして扱う
- イベント時刻は展開エントリの `timestamp` を採用する

縮退:
`parentUuid` が空、またはコマンドエントリの索引に見つからない場合、パスの basename から `Name: basename`、`Command: "/basename"` を組み立てる。
警告は出さず描画を続行する。
一つのコマンドエントリが複数の展開から参照されることは想定しない (parentUuid 一致のみで対応付け、回数制限は設けない)。

### 変えないこと

- `/clear` などスキル展開を伴わないコマンドエントリは従来どおり非表示
- `<command-name>` 等のノイズ除去 (noise.go) は従来どおり
- サイドチェイン除外・tool_result 統合などの既存フローは変更しない

## 描画

### md (templates/default.md.tmpl)

エージェント発動 — 発言UIの外に単独行 (tool_call と同じ並びの位置):

```markdown
🔧 Skill(superpowers:brainstorming) `/Users/.../superpowers/6.1.1/skills/brainstorming`
```

ユーザー呼び出し — 👤 User の発言UIとして描画し、本文にコマンド再現とチップを埋め込む:

```markdown
## 👤 User (11:22)

/ohayou

🔧 Skill(ohayou) `/Users/.../.claude/skills/ohayou`
```

### html (templates/default.html.tmpl)

スキルチップ用スタイル `.skill` を追加する (tool 行に近い控えめなトーン)。
パスはリンクにせず、そのままテキスト表示し、隣にコピーボタンを置く
(パスはスキルのディレクトリを指すためブラウザで開いても実用にならない。ターミナルで使う想定でコピー可能にする)。

- エージェント発動: `🔧 Skill(<b>名前</b>)` チップとパスのテキスト + コピーボタンを発言バブルの外に置く
- ユーザー呼び出し: `<div class="msg user">` の中に引数込みのコマンド再現 (例: `/ohayou`) と、
  その下に同じスキルチップを埋め込む。ユーザー発動分はタイムライン (nav.toc) にもコマンドをラベルとして列挙する

## 統計

- `Stats.SkillInvocations` を数え、両テンプレートのイベント集計行に「スキル N」を追加する
- エージェント発動で `skill_invocation` になった Skill tool_use は `ToolCalls` に数えない (二重計上しない)
- ユーザー呼び出しの `skill_invocation` は `UserMessages` にも数えない (SkillInvocations のみ)

## テスト方針

### parse ユニットテスト

1. エージェント発動 (Skill tool_use + sourceToolUseID 付き展開) が1つの `skill_invocation` になる
2. ユーザー呼び出し (command エントリ + 展開) が ByUser=true・Command 再現付きになる
3. 引数付きコマンド (`<command-args>`) の Command 再現
4. コマンドエントリ欠落時 (parentUuid が索引に無い / 空) の basename 縮退
5. 拒否された Skill tool_use は従来どおり permission_deny のまま
6. 展開本文が user_message として出力に漏れない
7. コマンドと展開の間に補助レコード (assistant 発話など) が挟まっても parentUuid で正しく結び付く
8. コマンド後に通常発話を挟んだ孤立展開 (parentUuid 不一致) が、古いコマンドと結び付かず basename 縮退になる
9. 連続する2件のユーザー呼び出しが、それぞれ正しいコマンドと結び付く

### render テスト

- md: チップの文字列形式 (インラインコード) とユーザー発言UIへの埋め込み
- html: `.skill` チップ・パスのテキスト表示 (`file://` リンクにしないこと)・ユーザーバブルへの埋め込み
- html: パスが %エスケープ等で加工されず原文どおり表示されること
- html: ユーザー発動スキルがタイムラインに列挙され、エージェント発動は列挙されないこと

### E2E (cmd/cctx/main_test.go)

既存フィクスチャにスキル展開行 (両発動元) を加え、md / html 出力を通しで確認する。

## ドキュメント (README)

README の外部テンプレート利用者向けセクションを更新する。

- イベント種別を6種から7種へ更新 (`skill_invocation` を追加)
- `Event.Skill` と `SkillInvocation` のフィールド一覧を追加
- `Stats.SkillInvocations` を追加
- デフォルトテンプレートでの表示例を追加
