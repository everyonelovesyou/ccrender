# サブコマンド CLI 設計書

日付: 2026-07-18
ステータス: 承認済み (2026-07-18)
関連: [全体設計書](2026-07-12-ccrender-design.md) の CLI 節を置き換える (マージ後に統合し本ファイルは削除)

## 目的

フラグの組み合わせ規則を単純にする。
現行 CLI は `--format` / `--stdout` / `-o` の矛盾指定を validate() の手書き検査で弾いているが、
出力形式をサブコマンドに畳み込むことで、矛盾を「構造的に表現できない」形にする。

機能追加はしない。既存機能の再配置のみ。

## CLI 仕様

### コマンド体系

```
ccrender md     [-o dir] [--template-md f]                     [--translate] <入力>
ccrender html   [-o dir]                   [--template-html f] [--translate] <入力>
ccrender both   [-o dir] [--template-md f] [--template-html f] [--translate] <入力>
ccrender stdout          [--template-md f]                     [--translate] <入力>
```

- `md` / `html` / `both` — 従来の `--format md|html|both` に対応。ファイル出力
- `stdout` — 従来の `--stdout` に対応。Markdown を標準出力へ (パイプ用)
- サブコマンドの省略は不可 (既定サブコマンドなし)

### 入力の指定 (全サブコマンド共通、現状維持)

```
<パス>                 # パス直接
<セッションID>          # 前方一致で ~/.claude/projects/ を探索
--latest [--project p] # 最新セッション (mtime 基準)、--project で絞り込み
```

入力解決の規則は全体設計書のとおり変更なし。

### ヘルプとエラーの挙動

原則: help 系の正常表示はすべて stdout (exit 0)、エラー起因の表示はすべて stderr (exit 1)。

| 呼び出し | 挙動 |
| --- | --- |
| `ccrender` (引数なし) | 使い方一覧を stderr へ、exit 1 |
| `ccrender -h` / `ccrender help` | 使い方一覧を stdout へ、exit 0 |
| `ccrender help <サブコマンド>` | そのサブコマンドのフラグ一覧を stdout へ、exit 0 |
| `ccrender <サブコマンド> -h` | そのサブコマンドのフラグ一覧を stdout へ、exit 0 |
| `ccrender foo` (未知) | 「未知のサブコマンドです: %q」+ 使い方一覧を stderr へ、exit 1 |
| 属さないフラグ (`ccrender stdout -o x` 等) | 未定義フラグエラーを stderr へ、exit 1 |

- 使い方一覧には用例 (`ccrender both --latest` 等) を必ず含める。
  旧形式 (`ccrender abc123` / `ccrender --latest`) は未知のサブコマンド経路に落ちるが、
  移行ヒントの特別扱いはしない。用例つき一覧で足りる
- FlagSet は `ContinueOnError` モードで生成し、パースエラーは error として
  main の既存エラー経路 (`ccrender: <エラー>` → exit 1) に合流させる。
  `-h` は `flag.ErrHelp` を `errors.Is` で拾って exit 0 に特別扱いする。
  `ExitOnError` は使わない (フラグ起因だけ exit 2 になり、os.Exit が flag 内部で起きてテストしにくい)
- フラグは位置引数より前に置く (Go flag は最初の非フラグ引数でパースを打ち切る、現行と同じ)。
  README にこの制約を明示する

### 検査規則の変化

構造的に消滅する検査 (validate() から削除):

- `--stdout` × `--format` の矛盾
- `--stdout` × `-o` の矛盾
- `--format` の値検査 (md|html|both 以外)

残る検査 (入力軸、現状維持):

1. 位置引数は1つのみ
2. `--latest` と位置引数の同時指定はエラー
3. `--project` は `--latest` と組み合わせたときのみ有効
4. 入力の指定なし (位置引数も `--latest` もなし) はエラー

## 実装構造

依存方針: 外部 CLI ライブラリ (cobra 等) は導入しない。
標準ライブラリの `flag.NewFlagSet` によるサブコマンド分岐で実装する。

```
main()
  → os.Args[1] で分岐 (なし / -h / help / 未知はここで処理)
  → parseSubcommand(name string, args []string) (*config, error)
      // サブコマンドごとに flag.NewFlagSet を組み立てる。
      // config.format / config.stdout はフラグではなくサブコマンド名から決まる
  → run(c, stdout, stderr)
```

- `config` 構造体は維持。`format` / `stdout` の値の出所がフラグからサブコマンド名に変わるのみ
- `validate()` は入力軸の4検査に縮小
- `run()` 以降のパイプライン (locate → parse → translate → render) は変更なし

### サブコマンドとフラグの対応

| サブコマンド | config.format | config.stdout | 固有フラグ |
| --- | --- | --- | --- |
| `md` | `md` | false | `-o`, `--template-md` |
| `html` | `html` | false | `-o`, `--template-html` |
| `both` | `both` | false | `-o`, `--template-md`, `--template-html` |
| `stdout` | `md` | true | `--template-md` |

共通フラグ: `--translate`, `--latest`, `--project`

## テスト

- `main_test.go` の組み合わせテーブルをサブコマンド形式へ書き換える。
  消えた矛盾規則のケースは「未定義フラグでエラー」のケースに置き換える
- 新規テーブルテスト: サブコマンド分岐
  - 4サブコマンド × format/stdout の対応が正しいこと
  - 引数なし / `-h` / `help` / 未知サブコマンドの exit・出力先
  - 残る検査4件がサブコマンド経由でも機能すること

## ドキュメント更新 (同一ブランチ内)

- README の「使い方」「フラグ一覧」「フラグの組み合わせ規則」節をサブコマンド形式に全面書き換え
- README の構成: 想定読者はエージェント。README だけで使い方が完結すること
  1. 共通事項 — 入力の指定3形態、共通フラグ (`--translate`)、「フラグは位置引数より前」の注意書き
  2. サブコマンド別の小節 (4つ) — 用例1〜2行 + 固有フラグの小さな表
  3. 組み合わせ規則 — 残る検査4件 (入力軸) のみの短い節に縮小

### マージ後

- 全体設計書の CLI 節を更新
- TODO.md の「サブコマンド形式にしたい!」を消し込み

## スコープ外

- 一覧系サブコマンド (list 等) の追加
- 入力選択の再設計 (`--latest` の位置引数化など)
- shell 補完
- フラグ後置の許容 (引数並べ替え)。
  `ccrender md abc123 --translate` の `--translate` は位置引数扱いになる (Go flag の仕様、現行と同じ挙動)
