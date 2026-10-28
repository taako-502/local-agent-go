# 社内資料管理APIの構成（架空のサンプル）

これは調査用の架空の仕様であり、local-agent-go自体の構成ではありません。社内資料管理APIの実装はこのリポジトリには含まれていません。

社内資料管理APIはGo、永続化はPostgreSQLです。全文検索にはPostgreSQLの検索機能を使います。開発環境のAPIはlocalhost:9090で動作し、GET /healthzで状態を確認する想定です。起動の前提と順序はsetup.mdを参照してください。

社内資料管理APIの認証は開発環境では無効、という設定です。本番設定はこの資料には含まれていません。

local-agent-goはTemporalとローカルLLMでこれらの資料を調べます。社内資料管理APIのPostgreSQLには接続しません。local-agent-goに付属するHello World APIは127.0.0.1:8090のGET /helloで、社内資料管理APIとは別のものです。
