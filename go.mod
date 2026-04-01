module github.com/MoChengqian/llm-access-gateway

go 1.26.1

require (
	github.com/go-chi/chi/v5 v5.2.5
	github.com/go-sql-driver/mysql v1.8.1
	github.com/spf13/viper v1.12.0
	go.uber.org/zap v1.27.0
)

require filippo.io/edwards25519 v1.1.0 // indirect

replace github.com/go-chi/chi/v5 => ./third_party/chi

replace github.com/spf13/viper => ./third_party/viper

replace go.uber.org/zap => ./third_party/zap
