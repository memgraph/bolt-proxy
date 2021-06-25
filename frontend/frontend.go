package frontend

import (
	"bytes"
	"net"
	"time"

	"github.com/memgraph/bolt-proxy/backend"
	"github.com/memgraph/bolt-proxy/bolt"
	"github.com/memgraph/bolt-proxy/proxy_logger"
)

const MAX_IDLE_MINS int = 30

// Identify if a new connection is valid Bolt or Bolt-over-Websocket
// connection based on handshakes.
//
// If so, wrap the incoming conn into a BoltConn and pass it off to
// a client handler
func HandleClient(conn net.Conn, backend_server *backend.Backend) {
	defer func() {
		proxy_logger.DebugLog.Printf("Closing client connection from %s\n",
			conn.RemoteAddr())
		conn.Close()
	}()
	// XXX why 1024? I've observed long user-agents that make this
	// pass the 512 mark easily, so let's be safe and go a full 1kb
	buf := make([]byte, 1024)

	data, err := conn.Read(buf[:4])
	if err != nil || data != 4 {
		proxy_logger.DebugLog.Println("Bad connection from", conn.RemoteAddr())
		return
	}
	if bytes.Equal(buf[:4], bolt.BoltSignature[:]) {
		// First case: we have a direct bolt client connection
		n, err := conn.Read(buf[:20])
		if err != nil {
			proxy_logger.DebugLog.Println("Error peeking at connection from", conn.RemoteAddr())
			return
		}
		// Make sure we try to use the version we're using the best
		// version based on the backend server
		serverVersion := backend_server.Version().Bytes()
		clientVersion, err := bolt.ValidateHandshake(buf[:n], serverVersion)
		if err != nil {
			proxy_logger.DebugLog.Fatal(err)
		}
		_, err = conn.Write(clientVersion)
		if err != nil {
			proxy_logger.DebugLog.Fatal(err)
		}
		// regular bolt
		proxy_logger.InfoLog.Println("Regular bolt")
		handleBoltConn(bolt.NewDirectConn(conn), clientVersion, backend_server)

	} else if bytes.Equal(buf[:4], bolt.HttpSignature[:]) {
		// Second case, we have an HTTP which only support health checks.
		// Read the rest of the request
		data, err = conn.Read(buf[4:])
		if err != nil {
			proxy_logger.DebugLog.Printf("Failed reading rest of GET request: %s\n", err)
			return
		}

		// Health check, maybe? If so, handle and bail.
		if IsHealthCheck(buf[:data+4]) {
			err = HandleHealthCheck(conn, buf[:data+4])
			if err != nil {
				proxy_logger.DebugLog.Println(err)
			}
			return
		}

	} else {
		// not bolt, not http...something else?
		proxy_logger.InfoLog.Printf("Client %s is speaking gibberish: %#v\n",
			conn.RemoteAddr(), buf[:4])
	}
}

// Primary Transaction client-side event handler, collecting Messages from
// the Bolt client and finding ways to switch them to the proper backend.
//
// The event loop...
//
// TOOD: this logic should be split out between the authentication and the
// event loop. For now, this does both.
func handleBoltConn(client bolt.BoltConn, clientVersion []byte, back *backend.Backend) {
	// Intercept HELLO message for authentication and hold onto it
	// for use in backend authentication
	var hello *bolt.Message
	proxy_logger.InfoLog.Printf("Version: %v\n", clientVersion)
	proxy_logger.InfoLog.Printf("Client: %v\n", client)
	select {
	case msg, ok := <-client.R():
		if !ok {
			proxy_logger.DebugLog.Println("Failed to read expected Hello from client", msg, ok)
			return
		}
		hello = msg
	case <-time.After(30 * time.Second):
		proxy_logger.DebugLog.Println("Timed out waiting for client to auth")
		return
	}
	proxy_logger.LogMessage("C->P", hello)

	if hello.T != bolt.HelloMsg {
		proxy_logger.DebugLog.Println("Expected HelloMsg, got:", hello.T)
		return
	}
	proxy_logger.DebugLog.Println("Expected HelloMsg, got:", hello.T)

	server_conn, err := back.InitBoltConnection(hello.Data, "tcp")
	if err != nil {
		proxy_logger.DebugLog.Println(err)
		return
	}

	v, _ := bolt.ParseVersion(clientVersion)
	proxy_logger.InfoLog.Printf("authenticated client %s speaking %s to %s server\n",
		client, v, back.MainInstance().Host)
	defer func() {
		proxy_logger.InfoLog.Printf("goodbye to client %s\n", client)
	}()

	// TODO: Replace hardcoded Success message with dynamic one
	success_msg := bolt.Message{
		T: bolt.SuccessMsg,
		Data: []byte{
			0x0, 0x2b, 0xb1, 0x70,
			0xa2,
			0x86, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72,
			0x8b, 0x4e, 0x65, 0x6f, 0x34, 0x6a, 0x2f, 0x34, 0x2e,
			0x32, 0x2e, 0x30,
			0x8d, 0x63, 0x6f, 0x6e, 0x6e, 0x65, 0x63, 0x74, 0x69, 0x6f, 0x6e, 0x5f, 0x69, 0x64,
			0x86, 0x62, 0x6f, 0x6c, 0x74, 0x2d, 0x34,
			0x00, 0x00}}
	proxy_logger.LogMessage("P->C", &success_msg)
	err = client.WriteMessage(&success_msg)
	if err != nil {
		proxy_logger.DebugLog.Fatal(err)
	}

	proxyListen(client, server_conn, back)
}

func proxyListen(client bolt.BoltConn, server bolt.BoltConn, back *backend.Backend) {
	// Time to begin the client-side event loop!
	var (
		startingTx bool = false
		manualTx   bool = false
		err        error
	)
	halt := make(chan bool, 1)
	ack := make(chan bool, 1)

	for {
		var msg *bolt.Message
		select {
		case m, ok := <-client.R():
			if ok {
				msg = m
				proxy_logger.LogMessage("C->P", msg)
			} else {
				proxy_logger.DebugLog.Println("potential client hangup")
				select {
				case halt <- true:
					proxy_logger.DebugLog.Println("client hangup, asking tx to halt")
				default:
					proxy_logger.DebugLog.Println("failed to send halt message to tx handler")
				}
				return
			}
		case <-time.After(time.Duration(MAX_IDLE_MINS) * time.Minute):
			proxy_logger.DebugLog.Println("client idle timeout")
			return
		}

		if msg == nil {
			// happens during websocket timeout?
			panic("msg is nil")
		}

		// Inspect the client's message to discern transaction state
		// We need to figure out if a transaction is starting and
		// what kind of transaction (manual, auto, etc.) it might be.
		switch msg.T {
		case bolt.BeginMsg:
			startingTx = true
			manualTx = true
		case bolt.RunMsg:
			if !manualTx {
				startingTx = true
			}
		case bolt.CommitMsg, bolt.RollbackMsg:
			manualTx = false
			startingTx = false
		}

		// XXX: This is a mess, but if we're starting a new transaction
		// we need to find a new connection to switch to
		proxy_logger.DebugLog.Printf("The incoming client message %v is manual: %t and startingTx: %t", msg.T, manualTx, startingTx)
		if startingTx {
			mode, _ := bolt.ValidateMode(msg.Data)

			var n int
			if msg.T == bolt.BeginMsg {
				proxy_logger.DebugLog.Print("proxy_logger.DebugLog begin MSG")
				_, _, err = bolt.ParseMap(msg.Data[4:])
				if err != nil {
					proxy_logger.DebugLog.Println(err)
					return
				}
			} else if msg.T == bolt.RunMsg {
				proxy_logger.DebugLog.Print("proxy_logger.DebugLog begin RUN")
				pos := 4
				// query
				_, n, err = bolt.ParseString(msg.Data[pos:])
				if err != nil {
					proxy_logger.DebugLog.Println(err)
					return
				}
				pos = pos + n
				// query params
				_, n, err = bolt.ParseMap(msg.Data[pos:])
				if err != nil {
					proxy_logger.DebugLog.Println(err)
					return
				}
				pos = pos + n
			} else {
				panic("shouldn't be starting a tx without a Begin or Run message")
			}

			// Todo Memgraph has no writers and readers one cluster has only one main
			readers := []string{back.MainInstance().Host}
			writers := []string{back.MainInstance().Host}

			var hosts []string
			if mode == bolt.ReadMode {
				hosts = readers
			} else {
				hosts = writers
			}
			if err != nil {
				proxy_logger.DebugLog.Printf("Couldn't find host")
			}

			if len(hosts) < 1 {
				proxy_logger.DebugLog.Println("No hosts")
				// TODO: return FailureMsg???
				return
			}
			host := hosts[0]

			// Are we already using a host? If so try to stop the
			// current tx handler before we create a new one
			if server != nil {
				select {
				case halt <- true:
					proxy_logger.DebugLog.Println("...asking current tx handler to halt")
					select {
					case <-ack:
						proxy_logger.DebugLog.Println("tx handler ack'd stop")
					case <-time.After(5 * time.Second):
						proxy_logger.DebugLog.Println("Timeout waiting for ack from tx handler")
					}
				default:
					// this shouldn't happen!
					panic("couldn't send halt to tx handler!")
				}
			}

			proxy_logger.DebugLog.Printf("grabbed conn for %s-access to db %s on host %s\n", mode, "ladida", host)

			// TODO: refactor channel handling...probably have handleTx() return new ones
			// instead of reusing the same ones. If we don't create new ones, there could
			// be lingering halt/ack messages. :-(
			halt = make(chan bool, 1)
			ack = make(chan bool, 1)

			// kick off a new tx handler routine
			go handleTx(client, server, ack, halt)
			startingTx = false
		}

		// TODO: this connected/not-connected handling looks messy
		if server != nil {
			err = server.WriteMessage(msg)
			if err != nil {
				// TODO: figure out best way to handle failed writes
				panic(err)
			}
			proxy_logger.LogMessage("P->S", msg)
		} else {
			// we have no connection since there's no tx...
			// handle only specific, simple messages
			switch msg.T {
			case bolt.ResetMsg:
				// XXX: Neo4j Desktop does this when defining a
				// remote dbms connection.
				// simply send empty success message
				client.WriteMessage(&bolt.Message{
					T: bolt.SuccessMsg,
					Data: []byte{
						0x00, 0x03,
						0xb1, 0x70,
						0xa0,
						0x00, 0x00,
					},
				})
			case bolt.GoodbyeMsg:
				// bye!
				return
			}
		}
	}
}

// Primary Transaction server-side event handler, collecting Messages from
// the backend Bolt server and writing them to the given client.
//
// Since this should be running async to process server Messages as they
// arrive, two channels are provided for signaling:
//
//  ack: used for letting this handler to signal that it's completed and
//       stopping execution, basically a way to confirm the requested halt
//
// halt: used by an external routine to request this handler to cleanly
//       stop execution
//
func handleTx(client, server bolt.BoltConn, ack chan<- bool, halt <-chan bool) {
	finished := false

	for !finished {
		select {
		case msg, ok := <-server.R():
			if ok {
				// logMessage("P<-S", msg)
				err := client.WriteMessage(msg)
				if err != nil {
					panic(err)
				}
				proxy_logger.LogMessage("C<-P", msg)

				// if know the server side is saying goodbye,
				// we abort the loop
				if msg.T == bolt.GoodbyeMsg {
					finished = true
				}
			} else {
				proxy_logger.DebugLog.Println("potential server hangup")
				finished = true
			}

		case <-halt:
			finished = true

		case <-time.After(time.Duration(MAX_IDLE_MINS) * time.Minute):
			proxy_logger.DebugLog.Println("Timeout reading server!")
			finished = true
		}
	}

	select {
	case ack <- true:
		proxy_logger.DebugLog.Println("Tx handler stop ACK sent")
	default:
		proxy_logger.DebugLog.Println("Couldn't put value in ack channel?!")
	}
}
