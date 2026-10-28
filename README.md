# local-agent-go

ローカルLLMとTemporalで、指定フォルダの資料を調べるGo製AIエージェント。

Goが資料ルートの一覧を渡し、LLMが毎回 `list_files` / `read_file` / `say_hello` / `finish` をJSONで選択します。Goがツールを実行し、その結果を次の判断に渡します。読む資料の順序はLLMが選びます。取得済みの操作を再選択した場合、`finish` を選んだ場合、調査回数の上限に達した場合は調査を終え、別のLLM呼び出しで文章の回答を生成します。ネイティブtool_callsではなく、学習しやすいJSONアクション方式です。

Goコードとテストは `cmd/local-agent-go/` に配置しています。以下のコマンドはリポジトリ直下で実行してください。

## 準備

Go 1.27以上が必要です。Temporal CLIはgo.modのtoolでバージョン管理しています。検証構成はApple Silicon Mac・llama.cpp・Qwen2.5-3B-Instruct Q4_K_Mです。モデル取得・依存関係取得時のみネット接続が必要です。クラウド契約不要です。

```sh
go mod download
go build -o local-agent-go ./cmd/local-agent-go
```

`go tool`が必要に応じてツールをビルドして実行するため、個別のインストールやPATH設定は不要です。初回のTemporal CLIビルドは、開発サーバーを含むため時間とディスク容量を使います。

Macでは[llama.cpp](https://github.com/ggml-org/llama.cpp)を別途インストールします。GoアプリからはOpenAI互換のローカルHTTP APIを呼びます。

```sh
brew install llama.cpp
```

## 起動（リポジトリ直下で別々のターミナル）

1. Temporal。履歴をディスクに保存します。

```sh
mkdir -p .local
go tool temporal server start-dev --ip 127.0.0.1 --db-filename .local/temporal.db
```

UI: http://localhost:8233

2. LLMサーバー。Metalを利用し、同時推論は1件にします。初回は約2GBのモデルを取得します。

```sh
llama-server -hf Qwen/Qwen2.5-3B-Instruct-GGUF \
  -hff qwen2.5-3b-instruct-q4_k_m.gguf \
  --alias local-agent --host 127.0.0.1 --port 8080 \
  -ngl 99 -c 8192 --parallel 1
```

同じGGUFが手元にあれば、`-hf` と `-hff` の代わりに `-m /path/to/model.gguf` を指定できます。以前tensaiで取得したMac上のモデルは、例えば `-m "$HOME/Library/Caches/tensai/qwen2.5-3b-instruct-q4_k_m.gguf"` で再利用できます。8080で別のLLMサーバーが動いていれば、先にそのサーバーを停止してください。

`--alias local-agent` がWorkerの既定の `-model local-agent` に対応します。別の互換サーバーを使う場合は `-llm` と `-model` を合わせてください。操作JSONの生成には `response_format: json_schema` を送信します。今回の実モデル検証はllama.cppで行い、tensaiのCPU実行で同じ速度・品質を保証するものではありません。

3. Worker。

```sh
./local-agent-go -mode worker -docs ./examples/docs
```

4. 質問。

```sh
./local-agent-go -question '資料に書かれた社内資料管理APIの起動手順とヘルスチェック方法を教えて'
```

`examples/docs` は、調査対象として用意した架空のプロジェクト「社内資料管理API」の資料です。資料中のPostgreSQL・マイグレーション・APIはこのリポジトリには実装していません。エージェントを試すために社内資料管理APIを起動する必要はありません。

質問CLIは10秒ごとに経過時間と「調査中」「資料取得中」「回答生成中」などの段階を表示します。Activityが再試行されている場合は試行回数も表示します。進行状況を取得できない場合はその旨を表示し、回答待ちは継続します。

期待する内容は、社内資料管理APIのPostgreSQL起動→マイグレーション→API起動、9090番ポートの /healthz、出典 setup.md です。実際の回答はモデル次第です。小型モデルがJSONを守れない場合は、同じモデルファミリーの大きなInstructモデルを使い、Workerの -model も合わせてください。

## 復旧を試す

```sh
./local-agent-go -id recovery-demo-1 -delay 30s -question '社内資料管理APIの構成と起動方法は？'
```

Workerのログにツールの選択が出た後、Temporal UIでActivity完了とTimer待機を確認してWorkerをCtrl+Cで止め、同じ引数で再起動します。完了記録のあるActivityの結果は再利用されます。途中だったActivityはタイムアウト後に再実行され得ます。質問CLIを止めてもWorkflowは残ります。

```sh
./local-agent-go -mode result -id recovery-demo-1
```

Server再起動時も同じDBファイルを指定してください。各実験には新しいIDを使います。Workflowコードの変更は既存履歴との互換性を考慮する必要があるため、最初は実行完了後に変更してください。

更新後はWorkerを再起動し、新しい質問を実行してください。更新前に開始したWorkflowは履歴の互換性を保つため従来の処理を続けます。既存のループを新しい処理に切り替えるものではありません。

## 実装と制限

- `Investigate`: 決定論的なWorkflow。LLM判断は既定で最大10回、`-steps` で1〜20回に設定できます。調査終了の `finish` も1回に数えます。取得済み操作の再選択または回数上限で回答生成へ進みます。成功した操作だけを記録し、根拠を1件も取得できなければエラーになります。
- `Activities.Plan`: 質問と観測結果をまとめて渡し、共通のツール一覧からLLMが次の操作を選びます。質問のキーワードによる分岐はありません。ツール名・引数・ファイルパス・API接続先はGo側でも検証します。
- 応答全体を囲むJSONのコード枠は取り除いて検証します。複数アクションなどの形式違反や調査前の回答には、その出力と修正指示をLLMに返します。1回のPlan内で初回を含め最大2回・合計90秒まで試し、修正できなかった場合はActivityの自動再試行も打ち切ります。通信障害などはTemporalの再試行対象です。
- `Activities.Answer`: 質問と取得済みの根拠だけを渡し、操作JSONではなく文章を生成します。HTTP要求は最大2分・1回のみです。Temporal側は実行130秒・待機込み150秒とし、HTTP側のエラーが先に返る余裕を設けます。失敗時は取得済みの資料・ツール一覧を含むエラーで終了し、生成失敗を成功として返しません。調査には戻りません。
- `Activities.Read`: os.Rootに制限された読み取り専用ツール。
- .md/.txt（UTF-8）、1ファイル8 KiB以下、1ディレクトリ200エントリ以下。
- 隠しパス・シンボリックリンク・ルート外アクセスは拒否。専用の資料フォルダを指定してください。
- Worker再起動時は同じ資料とモデルを使用します。異なる資料のWorkerは -queue を分け、質問側にも指定します。
- 文書の内容・質問・回答はTemporalの履歴に保存されます。これは学習用のローカル構成で、認証や複数利用者向け権限制御は未実装です。
- PDF、ベクトル検索、書き込み、シェル実行は含みません。
- JSON出力失敗は明示的にエラーにします。文書中の指示は無視するようプロンプトで指定し、実行可能な操作はコード側で限定しています。

## テスト

```sh
go mod tidy
go test ./...
go vet ./...
```

実際のTemporal・LLM・Worker・sample-apiを起動した状態で、次のテストを実行できます。資料のみ・APIのみ・複合依頼を順番に実行し、回答の主要な事実と実際に呼び出したActivityを検証します。通常の `go test` ではスキップします。

```sh
LOCAL_AGENT_LIVE=1 go test ./cmd/local-agent-go -run '^TestLiveAgent$' -v -count=1
```

検証Workerを別のキューで起動した場合は `LOCAL_AGENT_QUEUE` にそのキュー名を指定します。小型モデルの一般的な回答精度を保証するテストではありません。Worker停止・再起動は上記手順で別途確認してください。

## Hello World APIを呼ぶサンプル

`say_hello` は実装済みのエージェント用ツール名で、HTTPの `GET /hello` を呼び出します。
資料ツールと同様にLLMが選択し、実行結果を次の判断と最終回答へ渡します。挨拶APIの成功だけで終了しないため、資料と組み合わせる依頼にも同じ処理で対応します。

追加のターミナルでAPIを起動します。DBは不要です。

```sh
go build -o local-agent-go ./cmd/local-agent-go
./local-agent-go -mode sample-api
```

API単体は次で確認できます。

```sh
curl http://127.0.0.1:8090/hello
# {"message":"Hello, World!"}
```

TemporalとLLMサーバーを起動した状態で、Workerを新しいバイナリで起動し直します。

```sh
./local-agent-go -mode worker -docs ./examples/docs
```

別のターミナルから質問します。

```sh
./local-agent-go -question 'say_helloで挨拶APIを呼んで、返ってきたメッセージを教えて'
```

LLMが `{"action":"say_hello"}` を選ぶと、GoのActivityが `GET /hello` を呼び、
取得結果をLLMの次の判断に渡します。WorkerログとTemporal UIでPlan→SayHello→Plan→Answerを確認できます。

```sh
./local-agent-go -question '挨拶APIを呼んで、資料の内容と合わせて説明して'
```

複合依頼ではSayHelloとReadの両方を実行し、その結果をAnswerがまとめます。
Workerの `-api` で接続先を変更できます（ループバックHTTPのoriginのみ）。
接続失敗・429・5xxは再試行対象、その他のHTTPエラーはLLMに観測結果として返します。

既存の実行中Workflowを完了させてから更新し、新しいWorkflowで試してください。
疑似LLMと実際のHTTP APIを組み合わせたWorkflowテストを含みます。
実モデルによる検証には上記の `TestLiveAgent` を使います。

## ツールのバージョン管理

- llama.cpp: 検証時 Homebrew 0.7.0（`llama-server --version` で確認）
- tensai: v0.0.35（旧構成を再現するためGo toolとして残しています）
- Temporal CLI: v1.9.1

```sh
llama-server --version
go tool tensai version
go tool temporal --version
```

ソースからビルドしたTemporal CLIのバージョン表示は `0.0.0-DEV` になります。使用するリリースはgo.modのv1.9.1で固定されています。

CLI v1.9.1が参照するcliext v0.0.0は公開されていないため、go.modのreplaceで
同じリリースコミット（1de87a9f2699）のcliextを指定しています。
CLIを更新するときはこの指定も対応するコミットへ更新します。
ツールとアプリは依存関係を共有し、この構成ではTemporal SDKはv1.47.0を使用します。

ツールの更新は `go get -tool <パッケージ>@<バージョン>` と `go mod tidy` で行い、
go.mod・go.sumをコミットしてください。利用者が準備のたびに更新する必要はありません。
