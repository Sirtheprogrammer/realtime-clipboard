module clipboard

go 1.26.5

// Directives for Heroku's Go buildpack (the non-container deploy path).
// Container deploys ignore these and use the Dockerfile.
// +heroku goVersion go1.26
// +heroku install ./cmd/server

require (
	github.com/coder/websocket v1.8.15
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.17.0 // indirect
	golang.org/x/text v0.29.0 // indirect
)
