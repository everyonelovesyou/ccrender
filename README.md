# ccrender

Claude Code のセッショントランスクリプト (`~/.claude/projects/*/*.jsonl`) を読み込み、AI向け Markdown または人間向け HTML にレンダリングして書き出す CLI ツールです。別セッションやサブエージェントへの文脈引き継ぎ、あるいは過去セッションの振り返りに使えます。

## インストール

Go 1.24以降が必要です。

```bash
# このリポジトリを clone した状態で
go install ./cmd/ccrender

# または、生成物を手元に置きたい場合
go build -o ccrender ./cmd/ccrender
```

## 使い方

```
ccrender <サブコマンド> [フラグ] <入力>
```

出力形式はサブコマンドで選びます。`ccrender help` で使い方一覧、`ccrender help <サブコマンド>` で各サブコマンドのフラグ一覧を表示します。

**注意: フラグは位置引数より前に置いてください。** Go 標準 flag の仕様により、最初の位置引数より後のフラグはフラグとして解釈されません (`ccrender md abc123 --translate` の `--translate` は位置引数扱いになりエラー)。

### 入力の指定 (全サブコマンド共通)

次の3形態です。

```bash
ccrender md path/to/session.jsonl        # パス直接指定
ccrender md 1a2b3c4d                     # セッションID の前方一致 (~/.claude/projects/ 以下を探索)
ccrender md --latest                     # 最新セッション (ファイル mtime 基準)
ccrender md --latest --project my-app    # プロジェクト名 (ディレクトリ名の部分一致) で絞った最新セッション
```

- 位置引数はまずファイルパスとして存在確認し、存在しなければセッションIDの前方一致として探索します。

### 共通フラグ (全サブコマンド共通)

| フラグ | 説明 |
| --- | --- |
| `--translate` | サブエージェントの英語プロンプト・回答を日本語訳 ([^1]) |
| `--latest` | 最新セッションを対象にする |
| `--project string` | `--latest` の対象をプロジェクト名で絞る |

### ccrender md — Markdown をファイルに書き出す

```bash
ccrender md --latest
ccrender md -o out abc123
```

| 固有フラグ | 説明 |
| --- | --- |
| `-o string` | 出力先ディレクトリ (デフォルト: カレント)。ファイル名は `<セッションID>.md` を自動命名 |
| `--template-md string` | Markdown 用のカスタムテンプレート |

### ccrender html — HTML をファイルに書き出す

```bash
ccrender html --latest
ccrender html -o out path/to/session.jsonl
```

| 固有フラグ | 説明 |
| --- | --- |
| `-o string` | 出力先ディレクトリ (デフォルト: カレント)。ファイル名は `<セッションID>.html` を自動命名 |
| `--template-html string` | HTML 用のカスタムテンプレート |

### ccrender both — Markdown と HTML の両方をファイルに書き出す

```bash
ccrender both --latest
ccrender both -o out abc123
```

| 固有フラグ | 説明 |
| --- | --- |
| `-o string` | 出力先ディレクトリ (デフォルト: カレント)。ファイル名は `<セッションID>.md` / `.html` を自動命名 |
| `--template-md string` | Markdown 用のカスタムテンプレート |
| `--template-html string` | HTML 用のカスタムテンプレート |

### ccrender stdout — Markdown を標準出力へ (パイプ用)

```bash
ccrender stdout --latest
ccrender stdout abc123 | pbcopy
```

| 固有フラグ | 説明 |
| --- | --- |
| `--template-md string` | Markdown 用のカスタムテンプレート |

### 組み合わせ規則 (矛盾指定はエラー)

出力形式の矛盾はサブコマンドの構造上指定できません (例: `ccrender stdout -o x` は未定義フラグエラー)。残る規則は入力の指定に関する次の4件です。

- 位置引数は1つのみ。2つ以上の指定はエラー
- `--latest` と位置引数の同時指定はエラー
- `--project` は `--latest` と組み合わせたときのみ有効。単独指定はエラー
- 入力の指定なし (位置引数も `--latest` もなし) はエラー

## テンプレート変数一覧

テンプレートには `Session` 構造体がそのまま渡されます。

### Session

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `ID` | `string` | セッションID |
| `ProjectPath` | `string` | レコードの `cwd` フィールドから取得したプロジェクトパス |
| `StartedAt` | `time.Time` | 最初のレコードの timestamp |
| `EndedAt` | `time.Time` | 最後のレコードの timestamp |
| `Events` | `[]Event` | 時系列順のイベント一覧 |
| `Stats` | `Stats` | イベント種別ごとの件数 (ヘッダー表示用) |
| `Models` | `[]string` | 登場順・重複なしのモデルIDの一覧 (ヘッダー表示用)。空なら該当行は表示しない |

### Stats

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `UserMessages` | `int` | ユーザー発話の件数 |
| `AssistantMessages` | `int` | アシスタント発話の件数 |
| `ToolCalls` | `int` | ツール呼び出しの件数 |
| `PermissionDenies` | `int` | 権限拒否の件数 |
| `SystemNotes` | `int` | システムノートの件数 |
| `SubagentCalls` | `int` | サブエージェント呼び出しの件数 |
| `SkillInvocations` | `int` | スキル発動の件数 |
| `SkippedLines` | `int` | パースできずスキップした行数 |

### Event

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Kind` | `string` | イベント種別。`user_message` / `assistant_message` / `tool_call` / `permission_deny` / `system_note` / `subagent_call` / `skill_invocation` の7種 |
| `Timestamp` | `time.Time` | イベント発生時刻 |
| `Text` | `string` | `user_message` / `assistant_message` / `system_note` のときの本文 |
| `Tool` | `*ToolCall` | `Kind` が `tool_call` / `permission_deny` のとき非 nil |
| `Subagent` | `*Subagent` | `Kind` が `subagent_call` のとき非 nil |
| `Skill` | `*SkillInvocation` | `Kind` が `skill_invocation` のとき非 nil |

### ToolCall

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Name` | `string` | ツール名 (`Bash`、`Edit` など) |
| `Summary` | `string` | ツールごとの要約 (Bash ならコマンド、Edit ならパスなど) |
| `Input` | `string` | 全パラメータの整形 JSON (HTML では折りたたみ表示用) |
| `Result` | `string` | tool_result 本文。対応する tool_result が無い場合 (中断等) は空 |
| `HasResult` | `bool` | tool_result との突き合わせに成功したか。false ならテンプレートは「(結果なし)」と表示 |
| `IsError` | `bool` | 結果がエラーだったか |
| `DenyReason` | `string` | 権限拒否時にユーザーが添えたメッセージ |
| `Diff` | `string` | `Edit` のとき `old_string` → `new_string` の擬似 unified diff (先頭5行ずつ + 省略行)。他ツールは空 |

### Subagent

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `AgentID` | `string` | `subagents/agent-<AgentID>.jsonl` のファイル名部分 |
| `AgentType` | `string` | meta.json の agentType (`general-purpose` など) |
| `Prompt` | `string` | 依頼プロンプト全文 (要約しない) |
| `Answer` | `string` | 最終回答全文 (要約しない) |

### SkillInvocation

| フィールド | 型 | 説明 |
| --- | --- | --- |
| `Name` | `string` | スキル名。エージェント発動は Skill ツールの `input.skill`、ユーザー呼び出しはコマンド名の先頭 `/` を除いた形 |
| `Path` | `string` | スキル本体のディレクトリ絶対パス |
| `ByUser` | `bool` | true ならユーザーのスラッシュコマンド呼び出し、false ならエージェントによる Skill ツール発動 |
| `Command` | `string` | `ByUser` のとき「/name 引数」の再現文字列。エージェント発動では空 |

デフォルトテンプレートでの表示例 (Markdown):

```md
🔧 Skill(superpowers:brainstorming) `/Users/.../skills/brainstorming`
```

ユーザー呼び出しは 👤 User の発言としてコマンド再現 (`/ohayou ございます` など) と上記チップを表示します。スキル展開の本文 (SKILL.md) はどの形式でも出力しません。

### テンプレート関数

| 関数 | シグネチャ | 説明 |
| --- | --- | --- |
| `truncateLines` | `truncateLines n s` | 文字列 `s` を先頭 `n` 行に切り詰め、`… (残りX行省略)` を付記する。パイプ記法では `{{.Tool.Result \| truncateLines 20}}` のように使う |
| `firstLine` | `firstLine s` | 文字列 `s` の先頭1行を返す (HTML の `<details>` サマリー用) |
| `join` | `join sep ss` | 文字列スライス `ss` を `sep` で連結する。パイプ記法では `{{join ", " .Models}}` のように使う |
| `isMultiline` | `isMultiline s` | 文字列 `s` が複数行かを返す。ツール要約 (`Tool.Summary`) の全文表示分岐に使う |
| `showsResult` | `showsResult tc` | `ToolCall` の結果ブロックを描画すべきかを返す。`Read` / `Edit` / `Write` の**成功**結果は定型文の再掲にすぎないため `false` (失敗時は `true`)。`{{if showsResult .Tool}}` のように使う |

### `--template-md` / `--template-html` による差し替え例

デフォルトテンプレートは `go:embed` でバイナリに同梱されていますが、`--template-md` / `--template-html` で外部ファイルに差し替えられます。
たとえばツール結果を10行までに切り詰めた Markdown テンプレート `custom.md.tmpl` を用意し、次のように使います。

````md
# セッション {{.ID}}

{{range .Events}}
{{- if eq .Kind "tool_call"}}
🔧 {{.Tool.Name}}: {{firstLine .Tool.Summary}}
{{- if .Tool.HasResult}}
```
{{truncateLines 10 .Tool.Result}}
```
{{- end}}
{{- end}}
{{- end}}
````

```bash
ccrender md --template-md custom.md.tmpl session.jsonl
```

## 既知の制限

- Markdown 出力 (`text/template`) はエスケープしません。発話中の ``` 等が出力構造を壊す可能性がありますが、AI向け用途 (トークン節約優先) では許容しています。
- thinking ブロックは出力しません。
- `--translate` は外部プロセスとして `claude` CLI (`claude -p`) を呼び出します。`claude` CLI が使える環境でのみ動作します。

<!-- 脚注 -->

[^1]: #既知の制限
