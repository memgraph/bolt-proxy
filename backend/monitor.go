package backend

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/memgraph/bolt-proxy/bolt"
	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

type Monitor struct {
	driver  *neo4j.Driver
	version bolt.Version
	host    string
}

// Our default Driver configuration provides:
// - custom user-agent name
// - ability to add in specific list of hosts to use for address resolution
func newConfigurer(hosts []string) func(c *neo4j.Config) {
	return func(c *neo4j.Config) {
		c.AddressResolver = func(addr neo4j.ServerAddress) []neo4j.ServerAddress {
			if len(hosts) == 0 {
				return []neo4j.ServerAddress{addr}
			}

			addrs := make([]neo4j.ServerAddress, len(hosts))
			for i, host := range hosts {
				parts := strings.Split(host, ":")
				if len(parts) != 2 {
					panic(fmt.Sprintf("invalid host: %s", host))
				}
				addrs[i] = neo4j.NewServerAddress(parts[0], parts[1])
			}
			return addrs
		}
		// TODO: wire into global version string
		c.UserAgent = "bolt-proxy/v0.3.0"
	}
}

// The Monitor server to provide the data about the used backend service (Memgraph or Neo4j)
func NewMonitor(user, password, uri string, hosts ...string) (*Monitor, error) {
	// Try immediately to connect to Neo4j
	auth := neo4j.BasicAuth(user, password, "")
	driver, err := neo4j.NewDriver(uri, auth, newConfigurer(hosts))
	if err != nil {
		return nil, err
	}

	version := bolt.Version{1, 0, 0}

	// Get the cluster members and ttl details
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host = host + ":7687"
	}

	monitor := Monitor{
		driver:  &driver,
		version: version,
		host:    host,
	}

	return &monitor, nil
}
