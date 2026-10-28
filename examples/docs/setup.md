# 社内資料管理APIの起動手順（架空のサンプル）

これは調査用の架空の仕様です。社内資料管理APIのソースコードやCompose設定はこのリポジトリには含まれておらず、以下のコマンドはここでは実行できません。local-agent-goの起動にはリポジトリ直下のREADME.mdを参照してください。

社内資料管理APIの開発環境にはGo 1.25以上とDockerが必要、という設定です。local-agent-go自体に必要なGo 1.27以上とは区別してください。

社内資料管理APIのリポジトリ直下で、以下の順に実行する想定です。

1. `docker compose up -d postgres` でPostgreSQLを起動し、接続可能になるまで待ちます。
2. `go run ./cmd/migrate` でマイグレーションを実行します。
3. `go run ./cmd/api` でAPIを起動します。

社内資料管理APIはlocalhost:9090で動きます。`GET http://localhost:9090/healthz` で状態を確認する想定です。これはlocal-agent-goのHello World API（127.0.0.1:8090のGET /hello）のヘルスチェックではありません。
