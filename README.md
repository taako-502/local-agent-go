# local-agent-go

ローカルLLM（mattn/tensai）とTemporalで、指定フォルダの資料を調べるGo製AIエージェント。

Goが資料ルートの一覧を渡し、LLMが毎回 `list_files` / `read_file` / `say_hello` / `finish` をJSONで選択します。Goがツールを実行し、その結果を次の判断に渡します。読む資料の順序や回答はLLMが選びます。ネイティブtool_callsではなく、学習しやすいJSONアクション方式です。

Goコードとテストは `cmd/local-agent-go/` に配置しています。以下のコマンドはリポジトリ直下で実行してください。

## 準備

Go 1.27以上が必要です。Temporal CLIとtensaiはgo.modのtoolでバージョン管理しています。モデル取得・依存関係取得時のみネット接続が必要です。クラウド契約不要です。

```sh
go mod download
go build -o local-agent-go ./cmd/local-agent-go
```

`go tool`が必要に応じてツールをビルドして実行するため、個別のインストールやPATH設定は不要です。初回のTemporal CLIビルドは、開発サーバーを含むため時間とディスク容量を使います。

tensaiのビルド条件は[公式ドキュメント](https://mattn.github.io/tensai/ja/guide/platforms/)も参照してください。まずCPUで動かし、GPU最適化は後から追加できます。

## 起動（リポジトリ直下で別々のターミナル）

1. Temporal。履歴をディスクに保存します。

```sh
mkdir -p .local
go tool temporal server start-dev --ip 127.0.0.1 --db-filename .local/temporal.db
```

UI: http://localhost:8233

2. tensai。初回はモデルをダウンロードします。

```sh
go tool tensai serve -model Qwen/Qwen2.5-3B-Instruct-GGUF/qwen2.5-3b-instruct-q4_k_m.gguf -addr 127.0.0.1:8080
```

既定は[Qwen2.5-3B-InstructのGGUF版](https://huggingface.co/Qwen/Qwen2.5-3B-Instruct-GGUF)です。取得サイズは約2GBです。1.5Bモデルでは操作の選択が不安定だったため、この構成でサンプル資料の質問への回答を確認しています。モデルを変える場合はWorkerの `-model` も合わせてください。

3. Worker。

```sh
./local-agent-go -mode worker -docs ./examples/docs
```

4. 質問。

```sh
./local-agent-go -question '資料に書かれた社内資料管理APIの起動手順とヘルスチェック方法を教えて'
```

`examples/docs` は、調査対象として用意した架空のプロジェクト「社内資料管理API」の資料です。資料中のPostgreSQL・マイグレーション・APIはこのリポジトリには実装していません。エージェントを試すために社内資料管理APIを起動する必要はありません。

質問CLIは10秒ごとに待機時間を表示します。実行状況はWorkerのログとTemporal UIで確認できます。

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

## 実装と制限

- `Investigate`: 決定論的なWorkflow。LLM判断は既定で最大10回、`-steps` で1〜20回に設定できます。最終回答の `finish` も1回に数え、上限内に到達しなければエラーになります。
- `Activities.Decide`: ローカルLLMへのHTTP呼び出し。Activityは初回を含め最大3回実行し、再試行不可のエラーでは打ち切ります。
- 応答全体を囲むJSONのコード枠は取り除いて検証します。複数アクションなどの形式違反や調査前の回答には、その出力と修正指示をLLMに返します。1回のDecide内で初回を含め最大3回・合計4分まで試し、修正できなかった場合はActivityの自動再試行も打ち切ります。通信障害などはTemporalの再試行対象です。
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

実モデルでの回答品質とWorker停止・再起動は、上記手順で別途確認してください。

## Hello World APIを呼ぶサンプル

`say_hello` は実装済みのエージェント用ツール名で、HTTPの `GET /hello` を呼び出します。
`say_hello` の成功結果に空でない `message` があれば、その内容を直接返して完了します。API実行後の回答生成にはLLMを呼びません。通信失敗やAPIエラーを成功として扱うことはありません。

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

Temporalとtensaiを起動した状態で、Workerを新しいバイナリで起動し直します。

```sh
./local-agent-go -mode worker -docs ./examples/docs
```

別のターミナルから質問します。

```sh
./local-agent-go -question 'say_helloで挨拶APIを呼んで、返ってきたメッセージを教えて'
```

LLMが `{"action":"say_hello"}` を選ぶと、GoのActivityが `GET /hello` を呼び、
取得結果をLLMへ渡して回答します。WorkerログとTemporal UIでDecide→SayHello→Decideを確認できます。
Workerの `-api` で接続先を変更できます（ループバックHTTPのoriginのみ）。
接続失敗・429・5xxは再試行対象、その他のHTTPエラーはLLMに観測結果として返します。

既存の実行中Workflowを完了させてから更新し、新しいWorkflowで試してください。
疑似LLMと実際のHTTP APIを組み合わせたWorkflowテストを含みます。
実モデルによるツール選択は別途確認してください。

## ツールのバージョン管理

- tensai: v0.0.35
- Temporal CLI: v1.9.1

```sh
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
