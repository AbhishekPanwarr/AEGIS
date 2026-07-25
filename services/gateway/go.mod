module aegis/services/gateway

go 1.22

require (
	aegis/pkg v0.0.0-00010101000000-000000000000
	github.com/redis/go-redis/v9 v9.7.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)

replace aegis/pkg => ../../pkg
