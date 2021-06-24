module github.com/memgraph/bolt-proxy

go 1.15

require (
	github.com/gobwas/httphead v0.0.0-20200921212729-da3d93bc3c58 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.0.4
	github.com/neo4j/neo4j-go-driver/v4 v4.0.0-beta2
)

replace github.com/neo4j/neo4j-go-driver/v4 v4.0.0-beta2 => github.com/neo4j/neo4j-go-driver/v4 v4.2.0
