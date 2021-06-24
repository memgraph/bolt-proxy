package backend

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"

	"github.com/memgraph/bolt-proxy/bolt"
)

type Backend struct {
	monitor  *Monitor
	tls      bool
	log      *log.Logger
	main_uri *url.URL
	// map of principals -> hosts -> connections
	connectionPool map[string]map[string]bolt.BoltConn
	// routingCache   map[string]RoutingTable
	// info           ClusterInfo
}

func NewBackend(logger *log.Logger, username, password string, uri string, hosts ...string) (*Backend, error) {
	tls := false
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "bolt+s", "bolt+ssc", "neo4j+s", "neo4j+ssc":
		tls = true
	case "bolt", "neo4j":
		// ok
	default:
		return nil, errors.New("invalid neo4j connection scheme")
	}

	monitor, err := NewMonitor(username, password, uri, hosts...)
	if err != nil {
		return nil, err
	}

	return &Backend{
		monitor:        monitor,
		tls:            tls,
		log:            logger,
		main_uri:       u,
		connectionPool: make(map[string]map[string]bolt.BoltConn),
		// routingCache:   make(map[string]RoutingTable),
		// info:           <-monitor.Info,
	}, nil
}

func (b *Backend) Version() Version {
	return b.monitor.Version
}

func (b *Backend) MainInstance() *url.URL {
	return b.main_uri
}

func (b *Backend) InitBoltConnection(hello []byte, network string) (bolt.BoltConn, error) {
	bolt_signature := []byte{0x60, 0x60, 0xb0, 0x17}
	clientVersion := b.Version().Bytes()
	address := b.monitor.Host
	useTls := b.tls
	var (
		conn net.Conn
		err  error
	)
	fmt.Println("Before sending")

	if useTls {
		conf := &tls.Config{}
		conn, err = tls.Dial(network, address, conf)
	} else {
		conn, err = net.Dial(network, address)
	}
	if err != nil {
		return nil, err
	}

	handshake := append(bolt_signature, clientVersion...)
	handshake = append(handshake, []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00}...)
	fmt.Printf("Before sending %x\n", handshake)
	fmt.Printf("El version %x\n", clientVersion)
	_, err = conn.Write(handshake)
	fmt.Println("After sending")
	if err != nil {
		msg := fmt.Sprintf("couldn't send handshake to auth server %s: %s", address, err)
		conn.Close()
		return nil, errors.New(msg)
	}

	// Server should pick a version and provide as 4-byte array
	// TODO: we eventually need version handling...for now ignore :-/
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n != 4 {
		msg := fmt.Sprintf("didn't get valid handshake response from auth server %s: %s", address, err)
		conn.Close()
		return nil, errors.New(msg)
	}

	// Try performing the bolt auth the given hello message
	_, err = conn.Write(hello)
	if err != nil {
		msg := fmt.Sprintf("failed to send hello buffer to server %s: %s", address, err)
		conn.Close()
		return nil, errors.New(msg)
	}

	n, err = conn.Read(buf)
	if err != nil {
		msg := fmt.Sprintf("failed to get auth response from auth server %s: %s", address, err)
		conn.Close()
		return nil, errors.New(msg)
	}

	msg := bolt.IdentifyType(buf)
	if msg == bolt.FailureMsg {
		// See if we can extract the error message
		r, _, err := bolt.ParseMap(buf[4:n])
		if err != nil {
			conn.Close()
			return nil, err
		}

		val, found := r["message"]
		if found {
			failmsg, ok := val.(string)
			if ok {
				conn.Close()
				return nil, errors.New(failmsg)
			}
		}
		conn.Close()
		return nil, errors.New("could not parse auth server response")
	} else if msg == bolt.SuccessMsg {
		// The only happy outcome! Keep conn open.
		bolt_connection := bolt.NewDirectConn(conn)
		return bolt_connection, nil
	}

	// Try to be polite and say goodbye if we know we failed.
	conn.Write([]byte{0x00, 0x02, 0xb0, 0x02})
	conn.Close()
	return nil, errors.New("unknown error from auth server")
}

// For now, we'll authenticate to all known hosts up-front to simplify things.
// So for a given Hello message, use it to auth against all hosts known in the
// current routing table.
//
// Returns an map[string] of hosts to bolt.BoltConn's if successful, an empty
// map and an error if not.
func (b *Backend) Authenticate(hello *bolt.Message) (bool, error) {
	if hello.T != bolt.HelloMsg {
		panic("authenticate requires a Hello message")
	}

	// TODO: clean up this api...push the dirt into Bolt package?
	data := hello.Data[4:]
	client_string, pos, err := bolt.ParseString(data)
	b.log.Printf("Client string %s", client_string)

	auth_data := data[pos:]
	msg, pos, err := bolt.ParseMap(auth_data)
	if err != nil {
		b.log.Printf("XXX pos: %d, hello map: %#v\n", pos, msg)
		panic(err)
	}
	principal, ok := msg["principal"].(string)
	if !ok {
		panic("principal in Hello message was not a string")
	}
	b.log.Println("found principal:", principal)

	// Try authing first with a Core cluster member before we try others
	// this way we can fail fast and not spam a bad set of credentials
	// info, err := b.ClusterInfo()
	// if err != nil {
	// 	return nil, err
	// }
	// TODO Remove auth from memgraph for now
	defaultHost := b.monitor.Host

	// b.log.Printf("trying to auth %s to host %s\n", principal, defaultHost)
	// conn, err := authClient(hello.Data, b.Version().Bytes(),
	// 	"tcp", defaultHost, b.tls)
	// if err != nil {
	// 	return nil, err
	// }
	b.log.Printf("Conencting to %v %v", b.main_uri.Scheme, b.monitor.Host)
	conn, err := net.Dial("tcp", b.monitor.Host)

	// // Ok, now to get the rest
	conns := make(map[string]bolt.BoltConn, 1)
	conns[defaultHost] = bolt.NewDirectConn(conn)

	//TODO Implement auth module here
	return true, nil
}
