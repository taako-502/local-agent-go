# サンプルプロジェクト 社内資料管理API

社内資料管理APIは、社内の資料を管理・検索する架空のAPIです。このフォルダはlocal-agent-goが読み取る調査用のサンプル資料で、社内資料管理APIの実装はこのリポジトリに含まれていません。

以下の仕様と手順は社内資料管理APIの設定です。local-agent-go自体の起動手順はリポジトリ直下のREADME.mdを参照してください。

- 起動手順・前提環境・ヘルスチェック: setup.md
- API・永続化・全文検索・認証の構成: architecture.md

社内資料管理APIはlocalhost:9090です。local-agent-goのHello World API（127.0.0.1:8090のGET /hello）とは別のものです。
