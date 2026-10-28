# local-agent-go

ローカルLLM（mattn/tensai）とTemporalで、指定フォルダの資料を調べるGo製AIエージェント。

LLMが毎回 `list_files` / `read_file` / `say_hello` / `finish` をJSONで選択します。Goがツールを実行し、その結果を次の判断に渡します。固定の検索順序や模擬回答は使いません。ネイティブtool_callsではなく、学習しやすいJSONアクション方式です。

## 準備

Go 1.27以上、Temporal CLI、tensaiが必要です。モデル取得・依存関係取得時のみネット接続が必要です。クラウド契約不要です。

```sh
brew install temporal
go install github.com/mattn/tensai/cmd/tensai@v0.0.35
go mod tidy
go build -o local-agent-go .
```

tensaiのビルド条件は[公式ドキュメント](https://mattn.github.io/tensai/ja/guide/platforms/)も参照してください。まずCPUで動かし、GPU最適化は後から追加できます。

## 起動（リポジトリ直下で別々のターミナル）

1. Temporal。履歴をディスクに保存します。

```sh
mkdir -p .local
temporal server start-dev --ip 127.0.0.1 --db-filename .local/temporal.db
```

UI: http://localhost:8233

2. tensai。初回はモデルをダウンロードします。

```sh
tensai serve -model Qwen/Qwen2.5-1.5B-Instruct -q4 -addr 127.0.0.1:8080
```

3. Worker。

```sh
./local-agent-go -mode worker -docs ./examples/docs
```

4. 質問。

```sh
./local-agent-go -question 'このプロジェクトの起動手順とヘルスチェック方法を教えて'
```

期待する内容は、PostgreSQL起動→マイグレーション→API起動、9090番ポートの /healthz、出典 setup.md です。実際の回答はモデル次第です。小型モデルがJSONを守れない場合は、同じモデルファミリーの大きなInstructモデルを使い、Workerの -model も合わせてください。

## 復旧を試す

```sh
./local-agent-go -id recovery-demo-1 -delay 30s -question 'Atlasの構成と起動方法は？'
```

Workerのログにツールの選択が出た後、Temporal UIでActivity完了とTimer待機を確認してWorkerをCtrl+Cで止め、同じ引数で再起動します。完了記録のあるActivityの結果は再利用されます。途中だったActivityはタイムアウト後に再実行され得ます。質問CLIを止めてもWorkflowは残ります。

```sh
./local-agent-go -mode result -id recovery-demo-1
```

Server再起動時も同じDBファイルを指定してください。各実験には新しいIDを使います。Workflowコードの変更は既存履歴との互換性を考慮する必要があるため、最初は実行完了後に変更してください。

## 実装と制限

- `Investigate`: 決定論的なWorkflow。LLM判断→ツール実行を最大10回繰り返します。
- `Activities.Decide`: ローカルLLMへのHTTP呼び出し。最大3回再試行。
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

追加のターミナルでAPIを起動します。DBは不要です。

```sh
go build -o local-agent-go .
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
