module aegis/services/audit-chain

go 1.22

require (
	aegis/pkg v0.0.0-00010101000000-000000000000
	github.com/jackc/pgx/v5 v5.6.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/text v0.14.0 // indirect
)

replace aegis/pkg => ../../pkg
