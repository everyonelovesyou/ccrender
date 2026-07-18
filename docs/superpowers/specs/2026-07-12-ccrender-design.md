# ccrender 設計書

Claude Code トランスクリプト描画ツール

- 日付: 2026-07-12
- ステータス: 承認済み (ブレインストーミングで節ごとに承認)
- 追記: 2026-07-12 設計レビュー + トランスクリプト実地調査の反映 (サブエージェント紐付け、データモデル拡張、フラグ規則ほか)
- 追記: 2026-07-14 スキル発動エントリの特別描画を統合 (EventKind 7種目 `skill_invocation` を追加。PR #2)
- 追記: 2026-07-16 描画改善4件を統合 (空アシスタントターンの見出し、複数行 Summary、モデル名統計、パス相対化。PR #3)
- 追記: 2026-07-18 ツール名を cctx から ccrender にリネーム (PR #4)
- 追記: 2026-07-18 CLI をサブコマンド形式に再設計 (`--format` / `--stdout` をサブコマンドに畳み込み。PR #5)

## 目的

Claude Code のセッショントランスクリプト (`~/.claude/projects/*/*.jsonl`) を読み、
テンプレートエンジンで次の2形式にレンダリングして書き出す CLI ツール。

- **AI向け Markdown** — 別セッションやサブエージェントへの文脈引き継ぎ用。トークン節約を優先
- **人間向け HTML** — 過去セッションの振り返り用。1ファイル完結で読みやすさを優先

## 方針

- 言語は Go、依存は標準ライブラリのみ (テンプレートは `text/template` / `html/template`)
- 単一パス構成: JSONL 読み込み → 正規化イベントモデル構築 → テンプレートレンダリング → 書き出し
- cc-log-metrics (ccmetrics) とはコードを共有しない。パース知見 (permission_deny の検出方法等) はドキュメント参照のみ
- パス非依存 (モジュール名・入力パスをハードコードしない)
- 失敗時はフォールバックせず報告する
- トランスクリプトは全量メモリに載せる (単一セッションが対象のため数10MB級でも問題ない想定。ストリーミング処理はしない)

## CLI

出力形式はフラグではなくサブコマンドで指定する (サブコマンドの省略は不可)。
矛盾するフラグの組み合わせ (`--stdout --format html` 等) を「構造的に表現できない」形にするための設計で、機能はフラグ時代と同一。

```
ccrender <サブコマンド> [フラグ] <入力>

サブコマンド:
  md      Markdown をファイルに書き出す
  html    HTML をファイルに書き出す
  both    Markdown と HTML の両方をファイルに書き出す
  stdout  Markdown を標準出力へ書く (パイプ用)
  help    使い方を表示する (help <サブコマンド> でフラグ一覧)

入力の指定 (全サブコマンド共通、3形態):
  ccrender md path/to/session.jsonl          # パス直接
  ccrender md bbfac067                       # セッションID (前方一致で ~/.claude/projects/ を探索)
  ccrender both --latest                     # 最新セッション
  ccrender both --latest --project Workspace # プロジェクト名で絞った最新
```

### サブコマンドとフラグの対応

| サブコマンド | 固有フラグ |
| --- | --- |
| `md` | `-o`, `--template-md` |
| `html` | `-o`, `--template-html` |
| `both` | `-o`, `--template-md`, `--template-html` |
| `stdout` | `--template-md` |

- 共通フラグ: `--translate`, `--latest`, `--project`
- `-o <dir>` — 出力先ディレクトリ (デフォルト: カレント)。ファイル名は `<セッションID>.md` / `.html` を自動命名
- `--template-md` / `--template-html` — 自作テンプレートへの差し替え
- `--translate` — サブエージェントの英語プロンプト/回答を claude -p で日本語訳
- フラグは位置引数より前に置く (Go flag は最初の非フラグ引数でパースを打ち切る)。README に明示済み

### 入力解決の規則

- 位置引数は、まずファイルパスとして存在確認し、存在すればパスとして扱う。
  存在しなければセッションID 前方一致として `~/.claude/projects/*/*.jsonl` を探索する
- `--latest` の「最新」はファイル mtime 基準 (末尾イベントの timestamp 読み取りより単純で、実用上十分)
- `--project` はプロジェクトディレクトリ名 (`-Users-...` 形式) への部分一致で絞り込む。
  ディレクトリ名は元パスの `/` 以外の文字も `-` に潰した不可逆エンコードのため、元パスへの復元はしない

### 検査規則 (入力軸のみ、矛盾指定は明示エラー)

出力軸の矛盾 (`--stdout` × `--format` 等) はサブコマンド化で構造的に消滅し、検査は入力軸の4件のみ。

1. 位置引数は1つのみ
2. `--latest` と位置引数の同時指定はエラー
3. `--project` は `--latest` と組み合わせたときのみ有効。単独指定はエラー
4. 入力の指定なし (位置引数も `--latest` もなし) はエラー

属さないフラグ (`ccrender stdout -o x` 等) は FlagSet の未定義フラグエラーで弾かれる。

### ヘルプとエラーの挙動

原則: help 系の正常表示はすべて stdout (exit 0)、エラー起因の表示はすべて stderr (exit 1)。

| 呼び出し | 挙動 |
| --- | --- |
| `ccrender` (引数なし) | 使い方一覧を stderr へ、exit 1 |
| `ccrender -h` / `ccrender help` | 使い方一覧を stdout へ、exit 0 |
| `ccrender help <サブコマンド>` / `ccrender <サブコマンド> -h` | フラグ一覧を stdout へ、exit 0 |
| `ccrender foo` (未知) | 「未知のサブコマンドです: %q」+ 使い方一覧を stderr へ、exit 1 |

- 使い方一覧には用例を必ず含める。旧形式 (`ccrender abc123` 等) は未知のサブコマンド経路に落ちるが、移行ヒントの特別扱いはしない (用例つき一覧で足りる)
- FlagSet は `ContinueOnError` モードで生成し、パースエラーは main の既存エラー経路 (exit 1) に合流させる。
  `-h` は `flag.ErrHelp` を `errors.Is` で拾って exit 0 に特別扱いする。
  `ExitOnError` は使わない (フラグ起因だけ exit 2 になり、os.Exit が flag 内部で起きてテストしにくい)

### 実装構造 (cmd/ccrender)

外部 CLI ライブラリ (cobra 等) は導入せず、標準ライブラリの `flag.NewFlagSet` によるサブコマンド分岐で実装する。

```
main()
  → os.Args[1] で分岐 (なし / -h / help / 未知はここで処理)
  → parseSubcommand(name string, args []string) (*config, error)
      // サブコマンドごとに flag.NewFlagSet を組み立てる
      // config.format / config.stdout はフラグではなくサブコマンド名から決まる
  → run(c, stdout, stderr)
```

## データモデル

テンプレートに渡す正規化モデル。

```go
type Session struct {
    ID          string
    ProjectPath string    // レコード内の cwd フィールドから取得 (ディレクトリ名からの復元は不可逆のため不可)
    StartedAt   time.Time // 最初のレコードの timestamp
    EndedAt     time.Time // 最後のレコードの timestamp
    Models      []string  // assistant レコードの message.model を登場順・重複なしで収集
    Events      []Event   // 時系列順
    Stats       Stats     // イベント種別ごとの件数 (ヘッダー表示用)
}

type Event struct {
    Kind      EventKind // 下記7種
    Timestamp time.Time
    Text      string    // 発話本文 (User/Assistant)
    Tool      *ToolCall // Kind が ToolCall/PermissionDeny のとき
    Subagent  *Subagent // Kind が SubagentCall のとき
    Skill     *SkillInvocation // Kind が SkillInvocation のとき
}

type SkillInvocation struct {
    Name    string // スキル名。エージェント発動は input.skill、ユーザー呼び出しは <command-name> の先頭 "/" を除いた形
    Path    string // "Base directory for this skill:" の絶対パス
    ByUser  bool   // true ならユーザー呼び出し
    Command string // ByUser のとき「/name 引数」の再現文字列 (引数込み)。エージェント発動では空
}

type ToolCall struct {
    Name       string // "Bash", "Edit" など
    Summary    string // ツールごとの要約 (Bash ならコマンド、Edit ならパス)
    Input      string // 全パラメータの整形 JSON (HTML では折りたたみ表示用)
    Result     string // tool_result 本文。対応する tool_result が無い場合 (中断等) は空
    HasResult  bool   // tool_result との突き合わせに成功したか。false ならテンプレートは「(結果なし)」と表示
    IsError    bool
    DenyReason string // 権限拒否時にユーザーが添えたメッセージ
}

type Subagent struct {
    AgentID   string // subagents/agent-<AgentID>.jsonl のファイル名部分
    AgentType string // meta.json の agentType ("general-purpose" など)
    Prompt    string // 依頼プロンプト全文 (要約しない)
    Answer    string // 最終回答全文 (要約しない)
}
```

- ツール結果の20行切り詰め (Markdown) と「サブエージェント回答は要約せず埋め込む」は
  `ToolCall.Result` と `Subagent.Answer` という別フィールドに分かれるため矛盾しない。
  テンプレートは `Result` にのみ `truncateLines` を適用する

### EventKind (7種)

1. `UserMessage` — ユーザーの発話
    - `<system-reminder>` ブロック、`<command-name>` などのスキル展開ノイズは除去
2. `AssistantMessage` — アシスタントの発話テキスト (thinking は含めない)。
    テキストが空でも tool_use を伴うターンには空テキストのイベントを発行する
    (見出しの帰属を守るため。「テキストも tool_use 由来の見出しもないターン」だけが新規に見出しを持ち、重複はしない)
3. `ToolCall` — tool_use と対応する tool_result を `tool_use_id` で突き合わせて1イベントに統合。
    対応する tool_result が無い tool_use (セッション中断等) は `HasResult=false` の ToolCall として出力する
4. `PermissionDeny` — tool_result が `is_error=true` かつ拒否文言の**前方一致**で検出
    (部分一致 grep は拒否文言を引用した行を誤検出する — ccmetrics の教訓)。
    添付メッセージは `DenyReason` に保持
5. `SystemNote` — compact 境界やセッション再開などの節目情報 (最小限)
6. `SubagentCall` — Agent ツールの呼び出し。通常の ToolCall ではなくこちらに分類する (下記)
7. `SkillInvocation` — スキル発動。展開エントリを発動元 (エージェント / ユーザー) に応じて専用チップとして描画する (下記)

### 権限拒否の検出仕様 (ccmetrics 2026-07-10 実データ調査より転記)

- 判定:
  `type=user` レコードの tool_result ブロックのうち `is_error=true` かつ content (文字列形) が次のプレフィックスに**先頭一致**:
  `The user doesn't want to proceed with this tool use.`
- 添付メッセージ:
  `the user said:\n` 以降を切り出し、`\n\nNote: The user's next message` (定型注意書きの固定プレフィックス) 以降を除去してトリム。無ければ空文字列
- 文言は現行1系統のみ。旧文言 `Permission for this tool use was denied.` は引用ノイズによる誤集計で実在しない
- `Permission to use X ... has been denied.` (権限ルールによる自動拒否) は PermissionDeny にせず通常の ToolCall (IsError=true) とする
- content がブロック配列形の場合はクラッシュせず非該当とする

### トランスクリプト形式の前提 (2026-07-12 実地調査)

- レコードの `type` は user / assistant / system / attachment / mode / ai-title / last-prompt 等。
  出力に使わない型・未知の型は読み飛ばし、件数のみ数える (ccmetrics と同方針)
- 各レコードは `cwd`, `sessionId`, `timestamp` (ISO 8601 UTC), `uuid`, `parentUuid`, `isSidechain` を持つ
- 本流ファイル内の `isSidechain=true` レコードは直近実データで未観測だが、防御的にスキップする

### サブエージェント (SubagentCall)

保存形式 (2026-07-12 実地調査):

- サブエージェントの記録は `<session_id>/subagents/agent-<agentId>.jsonl` + 同名 `.meta.json` の別ファイル
- `meta.json` は `{agentType, description, toolUseId, spawnDepth}` を持ち、**`toolUseId` が本流の Agent tool_use の `id` と1対1対応する** (並行実行でも一意に紐付く)

構築手順:

1. 本流で `name=="Agent"` の tool_use を見つけたら `SubagentCall` イベントとする
2. `Prompt` は tool_use の `input.prompt` から取得
3. `subagents/*.meta.json` を走査して `toolUseId` が一致するものを探し、対応する `agent-<agentId>.jsonl` の**最後の assistant レコードの text ブロック連結**を `Answer` とする。
    本流側の tool_result はハーネスが `agentId:` 行と `<usage>` タグを付加するため使わない
4. 対応する subagents/ ファイルが無い場合はエラー終了せず、本流 tool_result の text を `Answer` に使い stderr に警告を出す
    (欠損セッションを変換不能にしないための明示的な縮退。無言のフォールバックはしない)

出力仕様:

- 本流の Agent 呼び出し位置に「プロンプト全文 + 最終回答全文」を要約せずに埋め込む
- サイドチェーンの中間過程 (ツール呼び出し等) は含めない
- `--translate` 指定時は日本語訳して残す。英語かどうかの判定はローカルで行わず `claude -p` に委ねる
  (実装フェーズの依頼プロンプトは日本語の計画ステップと英語の定型文が混在するため、文字種ヒューリスティックでは破綻する)。
  詳細はレンダリング仕様の「翻訳」を参照

### スキル発動 (SkillInvocation)

スキル発動後、トランスクリプトにはスキル本文が user エントリとして記録される
(本文は `Base directory for this skill: <絶対パス>` で始まる)。
これを User 発言として描画せず、発動元に応じた専用チップとして描画する。
展開本文 (スキルの手順書) はどちらの形式でも丸ごと非表示 (折りたたみにも残さない)。

#### 発動元の判別 (2026-07-12 実地調査)

スキル展開エントリはどちらも `type: "user"` かつ `isMeta: true` だが、フィールド構成が異なる。

| 発動元 | 手がかり |
|---|---|
| ユーザー呼び出し (スラッシュコマンド) | `sourceToolUseID` を持たない。`parentUuid` が `<command-name>` / `<command-args>` を含む通常 user エントリの `uuid` を直接指す |
| エージェント発動 (Skill ツール) | `sourceToolUseID` を持ち、assistant の `tool_use (name: "Skill")` を指す。スキル名は `input.skill` |

#### 検出ロジック

- 展開エントリの判定: `type: "user"` かつ `isMeta: true` かつ最初の text ブロックが
  `Base directory for this skill: ` で始まること。パスはその行の残りから取得し、行末の `\r` と前後の空白を除去する。
  合致した展開エントリは `user_message` として出力しない。それ以外の isMeta エントリの扱いは変えない
- エージェント発動: 事前パスで展開エントリを `sourceToolUseID` で索引化し、
  `Skill` の tool_use をイベント化する際に対応する展開があれば `tool_call` の代わりに `skill_invocation` を出す
- ユーザー呼び出し: 事前パスで `<command-name>` を含む通常 user エントリを `uuid` で索引化し、
  展開エントリの `parentUuid` で引いて `skill_invocation` (ByUser=true) を出す。
  「直近のコマンドを覚えておく」近接方式は採らない (欠損・変則トランスクリプトで誤った対応付けが起きるため)。
  `Command` は「/name 引数」を再現し (`<command-args>` が空白のみなら引数なし)、イベント時刻は展開エントリの `timestamp` を採用する

#### 縮退

- 対応する展開が無い Skill tool_use (権限拒否された場合など) は従来どおり `tool_call` / `permission_deny` のまま。
  permission_deny 判定の優先順は維持する
- `input.skill` が空の場合、Name はパスの basename で代替する。空でなければ常に `input.skill` を優先する
- 展開エントリが `sourceToolUseID` を持つのに対応する Skill tool_use が本流に無い場合、その展開エントリは描画しない
- ユーザー呼び出しで `parentUuid` が空、またはコマンドエントリの索引に見つからない場合、
  パスの basename から `Name: basename`、`Command: "/basename"` を組み立て、警告は出さず描画を続行する

#### 描画と統計

- チップの形式: `🔧 Skill(skill-name) <絶対パス>`。パスは html ではテキスト表示 + コピーボタン
  (スキルパスはディレクトリを指すため `file://` リンクにしない)、md ではインラインコード表記
- エージェント発動: チップを発言UIの外 (tool_call と同じ並びの位置) に描画する
- ユーザー呼び出し: ユーザー発言UIに「/skill-name 引数」のコマンド再現とチップを埋め込む。
  html ではタイムライン (nav.toc) にもコマンドをラベルとして列挙する (エージェント発動は列挙しない)
- `Stats.SkillInvocations` を数え、両テンプレートのイベント集計行に「スキル N」を出す。
  `skill_invocation` になった Skill tool_use は `ToolCalls` に、ユーザー呼び出し分は `UserMessages` に数えない (二重計上しない)
- `/clear` などスキル展開を伴わないコマンドエントリは従来どおり非表示

## レンダリング仕様

1. ツール結果の扱い
    - Markdown: 先頭20行で切り、`… (残りX行省略)` を付記
      (切り詰めはテンプレート関数 `truncateLines` として提供し、行数は自作テンプレート側で変更可能)
    - HTML: 全文を `<details>` の折りたたみに収録し、閉じた状態でサマリー1行を表示
    - `HasResult=false` (tool_result 欠落) は「(結果なし)」と表示
    - 切り詰め対象は `ToolCall.Result` のみ。`Subagent.Prompt` / `Answer` は両形式とも全文
      (HTML では `<details>` 折りたたみ可)
2. ツール呼び出しの表示
    - `Summary` を常時表示。`Input` の全パラメータは HTML では折りたたみ、Markdown では省略
    - Summary が複数行の場合 (heredoc を使った Bash など)、Markdown はコードフェンスで全文表示、
      1行なら現状どおりインライン表示。複数行判定はテンプレート関数 `isMultiline` で行い、Bash に限定せず全ツールに適用する
    - HTML は `<summary>` 内のため視覚上は CSS で1行にクランプし、コピー対象 (copy-src) を全文とする。
      md のフェンス全文表示と見え方が異なるのは意図的 (折りたたみ UI では1行表示が自然で、全文は展開した input / コピーで取得できる)
    - `ProjectPath` 配下の絶対パスは Summary 組み立て時に相対パスへ短縮する (`filepath.Rel` ベース)。
      ルート外のパス (`~/.claude/...`、`/tmp/...` など) はそのまま。対象はパスを Summary に採用するツール (Read / Edit / Write 等) で、
      Bash のコマンド文字列は書き換えない (コマンドの再現性を優先する)
3. 権限拒否
    - 明示マーク (例: 🚫 拒否) と `DenyReason` を両形式で必ず表示
    - HTML では複数行の Summary も視覚1行クランプ + 全文コピーボタンで扱う (ツール呼び出しと同方針)
4. 翻訳 (`--translate`)
    - `claude -p --model haiku` を外部プロセスとして呼び出す。無指定なら原文のまま
    - 対象は `Subagent.Prompt` と `Subagent.Answer` のみ
    - 英語判定はローカルで行わない。
      翻訳プロンプトで「既に日本語主体のテキストはそのまま返す」よう指示し、判定を LLM に委ねる (日英混在テキストへの頑健性のため)
    - **バッチング**:
      1テキスト=1回の逐次起動は分オーダーの遅延になるため、全対象テキストを区切りマーカー付きで1回の呼び出しにまとめ、マーカーで分割して回収する。
      回収数が対象数と一致しなければエラー終了
    - タイムアウト (デフォルト120秒) 超過・プロセス失敗はフォールバックせずエラー終了 (部分成功の混在を避ける)

## テンプレート

- Markdown: `text/template`、HTML: `html/template` (自動エスケープ付き)
- デフォルトテンプレートは `go:embed` でバイナリに同梱。
  `--template-md` / `--template-html` で外部ファイルに差し替え可能
- テンプレートに渡るのは `Session` 構造体そのもの。README に変数一覧を記載
- テンプレート関数: `truncateLines` (先頭N行切り詰め + 省略行数付記)、`firstLine` (HTML の `<details>` サマリー用)
- Markdown 出力 (`text/template`) はエスケープしない。発話中の ``` 等が出力構造を壊し得るが、
  AI向け用途では許容する (既知の制限として README に記載)
- デフォルト HTML: 1ファイル完結 (CSS 埋め込み・外部依存なし)。発話は色分けのチャット風、
  ツール呼び出しはコンパクトな行 + 折りたたみ、ヘッダーにセッション概要
  (日時・プロジェクト・イベント件数・拒否件数・モデル名)
- モデル名は `Session.Models` を登場順にカンマ区切りで列挙する。1つも取れないセッションではモデル行を出さない
  (サブエージェント内部のモデルはメインループのレコードに現れないため、自然にメインループのモデルのみが対象になる)

## エラー処理

1. 入力解決
    - セッションIDが0件一致 → 探索したパスを添えてエラー終了
    - 複数一致 → 候補一覧を提示して終了
    - `--latest` (+ `--project`) で1件も見つからない → 探索条件を添えてエラー終了
    - フラグの矛盾指定 (CLI 節の組み合わせ規則参照) → 明示エラー
2. パース
    - 不正な JSONL 行はスキップし、終了時に「X行スキップ」を stderr へ。全行失敗なら異常終了
3. レンダリング・書き出し
    - 外部テンプレートの構文エラーは行番号付きで報告。出力先に書けない場合も報告
4. 翻訳
    - `claude -p` の失敗はエラー終了

## テスト

1. パーサー
    - 実トランスクリプト由来の小さな fixture JSONL でイベント抽出をユニットテスト (permission_deny・sidechain・壊れ行・tool_result 欠落を含む)
    - fixture は実ログから転記せず、個人情報を含まない捏造データで作成する (ccmetrics と同方針)
2. レンダラー
    - golden file テスト: fixture → 期待される .md / .html と比較
    - 空セッション・発話ゼロ (ツール呼び出しのみ) のセッションでテンプレートが壊れないことを含める
3. 翻訳
    - `claude -p` はインターフェースで抽象化しモックでテスト (実呼び出しはテスト対象外)

## リポジトリ構成

`~/Personal/develop/ccrender`

```
ccrender/
├── cmd/ccrender/main.go    # CLI エントリ (flag 解釈のみ)
├── internal/
│   ├── locate/             # 入力解決 (パス/ID/--latest)
│   ├── parse/              # JSONL → Session モデル
│   ├── render/             # テンプレートレンダリング
│   └── translate/          # claude -p 呼び出し (interface + 実装)
├── templates/              # デフォルトテンプレート (go:embed)
│   ├── default.md.tmpl
│   └── default.html.tmpl
└── docs/superpowers/specs/ # 設計書の置き場
```

## スコープ外

- サイドチェーン内部の詳細な展開
- thinking ブロックの出力
- 複数セッションの一括変換・集計 (ccmetrics の領分)
- フラグ後置の許容 (引数並べ替え)。
  `ccrender md abc123 --translate` の `--translate` は位置引数扱いになる (Go flag の仕様)
