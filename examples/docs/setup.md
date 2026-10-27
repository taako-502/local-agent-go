# 起動手順

Go 1.25以上とDockerが必要です。
1. docker compose up -d postgres
2. go run ./cmd/migrate
3. go run ./cmd/api

APIはlocalhost:9090で動きます。GET /healthzで状態を確認できます。
