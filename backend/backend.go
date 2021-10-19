/*
Copyright (c) 2021 Memgraph Ltd. [https://memgraph.com]

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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
	bindOn             string
	proxyTo            string
	username, password string
	certFile, keyFile  string
	debugMode          bool
}

type Backend struct {
	monitor        *Monitor
	main_uri       *url.URL
	auth           Authenticator
	connectionPool map[string]map[string]bolt.BoltConn
	tls            bool
}

func NewBackend(username, password, uri string, auth Authenticator, hosts ...string) (*Backend, error) {
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
		return nil, errors.New("invalid bolt connection scheme")
	}

	monitor, err := NewMonitor(username, password, uri, hosts...)
	if err != nil {
		return nil, err
	}

	return &Backend{
		monitor:        monitor,
		tls:            tls,
		main_uri:       u,
		auth:           auth,
		connectionPool: make(map[string]map[string]bolt.BoltConn),
	}, nil
}

func (b *Backend) Version() bolt.Version {
	return b.monitor.version
}

func (b *Backend) MainInstance() *url.URL {
	return b.main_uri
}

func (b *Backend) IsAuthEnabled() bool {
	return b.auth != nil
}

func (b *Backend) InitBoltConnection(hello []byte, network string) (bolt.BoltConn, error) {
	backend_version := b.Version().Bytes()
	address := b.monitor.host
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

	handshake := append(bolt.BoltSignature[:], backend_version...)
	handshake = append(handshake, []byte{
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00}...)
	_, err = conn.Write(handshake)

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
	switch msg {
	case bolt.FailureMsg:
		// See if we can extract the error message
		r, _, errParse := bolt.ParseMap(buf[4:n])
		if errParse != nil {
			conn.Close()
			return nil, errParse
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
	case bolt.SuccessMsg:
		// The only happy outcome! Keep conn open.
		bolt_connection := bolt.NewDirectConn(conn)
		return bolt_connection, nil
	}

	// Try to be polite and say goodbye if we know we failed.
	_, err = conn.Write([]byte{0x00, 0x02, 0xb0, 0x02})
	if err != nil {
		return nil, fmt.Errorf("write: %v", err)
	}

	err = conn.Close()
	if err != nil {
		return nil, fmt.Errorf("close: %v", err)
	}

	return nil, errors.New("unknown error from auth server")
}

// This part can be extended with third party auth service, so that Memgraph does not perform auth
func (b *Backend) Authenticate(hello *bolt.Message) error {
	if hello.T != bolt.HelloMsg {
		panic("authenticate requires a Hello message")
	}

	// TODO: clean up this api...push the dirt into Bolt package?
	data := hello.Data[4:]
	client_string, pos, err := bolt.ParseString(data)
	if err != nil {
		return fmt.Errorf("parse: %v", err)
	}
	proxy_logger.DebugLog.Printf("client string %s", client_string)

	auth_data := data[pos:]
	msg, pos, err := bolt.ParseMap(auth_data)
	if err != nil {
		proxy_logger.DebugLog.Printf("XXX pos: %d, hello map: %#v\n", pos, msg)
		panic(err)
	}

	if b.auth != nil {
		return b.auth.Authenticate(msg)
	}
	return nil
}
