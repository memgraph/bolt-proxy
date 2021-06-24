package backend

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/neo4j/neo4j-go-driver/v4/neo4j"
)

// TODO: what the hell are we doing here?
type Monitor struct {
	// Info    <-chan ClusterInfo
	// halt    chan bool
	driver  *neo4j.Driver
	Version Version
	// Ttl     time.Duration
	Host string
}

type Version struct {
	Major, Minor, Patch uint8
	Extra               string
}

func ParseVersion(buf []byte) (Version, error) {
	if len(buf) < 4 {
		return Version{}, errors.New("buffer too short (< 4)")
	}

	version := Version{}
	version.Major = uint8(buf[3])
	version.Minor = uint8(buf[2])
	version.Patch = uint8(buf[1])
	return version, nil
}

func (v Version) String() string {
	return fmt.Sprintf("Bolt{major: %d, minor: %d, patch: %d, extra: %s}",
		v.Major,
		v.Minor,
		v.Patch,
		v.Extra)
}

func (v Version) Bytes() []byte {
	return []byte{
		0x00, 0x00,
		v.Minor, v.Major,
	}
}

// func (m Monitor) UpdateRoutingTable(db string) (RoutingTable, error) {
// 	return getRoutingTable(m.driver, db, m.Host)
// }

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

// Construct and start a new routing table Monitor using the provided user,
// password, and uri as arguments to the underlying neo4j.Driver. Returns a
// pointer to the Monitor on success, or nil and an error on failure.
//
// Any additional hosts provided will be used as part of a custom address
// resolution function via the neo4j.Driver.
func NewMonitor(user, password, uri string, hosts ...string) (*Monitor, error) {
	// infoChan := make(chan ClusterInfo, 1)
	// haltChan := make(chan bool, 1)

	// Try immediately to connect to Neo4j
	auth := neo4j.BasicAuth(user, password, "")
	driver, err := neo4j.NewDriver(uri, auth, newConfigurer(hosts))
	if err != nil {
		return nil, err
	}

	// version, err := getVersion(&driver)
	version := Version{1, 0, 0, "community"}
	// if err != nil {
	// 	panic(err)
	// }

	// TODO: check if in SINGLE, CORE, or READ_REPLICA mode
	// We can run `CALL dbms.listConfig('dbms.mode') YIELD value` and
	// check if we're clustered or not. Ideally, if not clustered, we
	// simplify the monitor considerably to just health checks and no
	// routing table.

	// Get the cluster members and ttl details
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	host := u.Host
	if u.Port() == "" {
		host = host + ":7687"
	}

	// info, err := getClusterInfo(&driver, host)
	// if err != nil {
	// 	return nil, err
	// }
	// infoChan <- info

	monitor := Monitor{
		driver:  &driver,
		Version: version,
		Host:    host,
	}

	return &monitor, nil
}
