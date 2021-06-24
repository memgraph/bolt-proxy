package backend

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"

	"github.com/memgraph/bolt-proxy/bolt"
	"github.com/memgraph/bolt-proxy/proxy_logger"
)

type Parameters struct {
	debugMode          bool
	bindOn             string
	proxyTo            string
	username, password string
	certFile, keyFile  string
}

type Backend struct {
	monitor        *Monitor
	tls            bool
	main_uri       *url.URL
	connectionPool map[string]map[string]bolt.BoltConn
}

func NewBackend(username, password, uri string, hosts ...string) (*Backend, error) {
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
		return nil, errors.New("Invalid bolt connection scheme")
	}

	monitor, err := NewMonitor(username, password, uri, hosts...)
	if err != nil {
		return nil, err
	}

	return &Backend{
		monitor:        monitor,
		tls:            tls,
		main_uri:       u,
		connectionPool: make(map[string]map[string]bolt.BoltConn),
	}, nil
}

func (b *Backend) Version() Version {
	return b.monitor.version
}

func (b *Backend) MainInstance() *url.URL {
	return b.main_uri
}

func (b *Backend) InitBoltConnection(hello []byte, network string) (bolt.BoltConn, error) {
	bolt_signature := []byte{0x60, 0x60, 0xb0, 0x17}
	backend_version := b.Version().Bytes()
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

	handshake := append(bolt_signature, backend_version...)
	handshake = append(handshake, []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00}...)
	_, err = conn.Write(handshake)

	if err != nil {
		msg := fmt.Sprintf("Couldn't send handshake to auth server %s: %s", address, err)
		conn.Close()
		return nil, errors.New(msg)
	}

	// Server should pick a version and provide as 4-byte array
	// TODO: we eventually need version handling...for now ignore :-/
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n != 4 {
		msg := fmt.Sprintf("Didn't get valid handshake response from auth server %s: %s", address, err)
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

// This part can be extended with third party auth service, so that Memgraph does not perform auth
func (b *Backend) Authenticate(hello *bolt.Message) (bool, error) {
	if hello.T != bolt.HelloMsg {
		panic("authenticate requires a Hello message")
	}

	// TODO: clean up this api...push the dirt into Bolt package?
	data := hello.Data[4:]
	client_string, pos, err := bolt.ParseString(data)
	proxy_logger.DebugLog.Printf("Client string %s", client_string)

	auth_data := data[pos:]
	msg, pos, err := bolt.ParseMap(auth_data)
	if err != nil {
		proxy_logger.DebugLog.Printf("XXX pos: %d, hello map: %#v\n", pos, msg)
		panic(err)
	}
	principal, ok := msg["principal"].(string)
	if !ok {
		panic("principal in Hello message was not a string")
	}
	proxy_logger.DebugLog.Println("found principal:", principal)

	defaultHost := b.monitor.Host

	proxy_logger.DebugLog.Printf("Conencting to %v %v", b.main_uri.Scheme, b.monitor.Host)
	conn, err := net.Dial("tcp", b.monitor.Host)

	// // Ok, now to get the rest
	conns := make(map[string]bolt.BoltConn, 1)
	conns[defaultHost] = bolt.NewDirectConn(conn)

	//TODO Implement auth module here
	return true, nil
}
