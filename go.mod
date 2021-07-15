module github.com/memgraph/bolt-proxy

go 1.15

require (
	github.com/coreos/go-oidc/v3 v3.0.0 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.0.4
	github.com/neo4j/neo4j-go-driver/v4 v4.3.1
)

replace github.com/neo4j/neo4j-go-driver/v4 v4.0.0-beta2 => github.com/neo4j/neo4j-go-driver/v4 v4.2.0
