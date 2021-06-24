package frontend

import (
	"bytes"
	"io"
	"net"
	"time"

	"github.com/gobwas/ws"
	"github.com/memgraph/bolt-proxy/backend"
	"github.com/memgraph/bolt-proxy/bolt"
	"github.com/memgraph/bolt-proxy/health"
)

const MAX_IDLE_MINS int = 30

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
				logMessage("C<-P", msg)

				// if know the server side is saying goodbye,
				// we abort the loop
				if msg.T == bolt.GoodbyeMsg {
					finished = true
				}
			} else {
				debug.Println("potential server hangup")
				finished = true
			}

		case <-halt:
			finished = true

		case <-time.After(time.Duration(MAX_IDLE_MINS) * time.Minute):
			warn.Println("timeout reading server!")
			finished = true
		}
	}

	select {
	case ack <- true:
		debug.Println("tx handler stop ACK sent")
	default:
		warn.Println("couldn't put value in ack channel?!")
	}
}

// Identify if a new connection is valid Bolt or Bolt-over-Websocket
// connection based on handshakes.
//
// If so, wrap the incoming conn into a BoltConn and pass it off to
// a client handler
func HandleClient(conn net.Conn, backend_server *backend.Backend) {
	defer func() {
		debug.Printf("Closing client connection from %s\n",
			conn.RemoteAddr())
		conn.Close()
	}()
	// XXX why 1024? I've observed long user-agents that make this
	// pass the 512 mark easily, so let's be safe and go a full 1kb
	buf := make([]byte, 1024)

	data, err := conn.Read(buf[:4])
	if err != nil || data != 4 {
		warn.Println("bad connection from", conn.RemoteAddr())
		return
	}
	if bytes.Equal(buf[:4], []byte{0x60, 0x60, 0xb0, 0x17}) {
		// First case: we have a direct bolt client connection
		n, err := conn.Read(buf[:20])
		if err != nil {
			warn.Println("error peeking at connection from", conn.RemoteAddr())
			return
		}
		// Make sure we try to use the version we're using the best
		// version based on the backend server
		serverVersion := backend_server.Version().Bytes()
		clientVersion, err := bolt.ValidateHandshake(buf[:n], serverVersion)
		if err != nil {
			warn.Fatal(err)
		}
		_, err = conn.Write(clientVersion)
		if err != nil {
			warn.Fatal(err)
		}
		// regular bolt
		info.Print("Regular bolt")
		handleBoltConn(bolt.NewDirectConn(conn), clientVersion, backend_server)

	} else if bytes.Equal(buf[:4], []byte{0x47, 0x45, 0x54, 0x20}) {
		// Second case, we have an HTTP connection that might just
		// be a WebSocket upgrade OR a health check.

		// Read the rest of the request
		data, err = conn.Read(buf[4:])
		if err != nil {
			warn.Printf("failed reading rest of GET request: %s\n", err)
			return
		}

		// Health check, maybe? If so, handle and bail.
		if health.IsHealthCheck(buf[:data+4]) {
			err = health.HandleHealthCheck(conn, buf[:data+4])
			if err != nil {
				warn.Println(err)
			}
			return
		}

		// Build something implementing the io.ReadWriter interface
		// to pass to the upgrader routine
		iobuf := bytes.NewBuffer(buf[:data+4])
		_, err := ws.Upgrade(iobuf)
		if err != nil {
			warn.Printf("failed to upgrade websocket client %s: %s\n",
				conn.RemoteAddr(), err)
			return
		}
		// Relay the upgrade response
		_, err = io.Copy(conn, iobuf)
		if err != nil {
			warn.Printf("failed to copy upgrade to client %s\n",
				conn.RemoteAddr())
			return
		}

		// After upgrade, we should get a WebSocket message with header
		header, err := ws.ReadHeader(conn)
		if err != nil {
			warn.Printf("failed to read ws header from client %s: %s\n",
				conn.RemoteAddr(), err)
			return
		}
		n, err := conn.Read(buf[:header.Length])
		if err != nil {
			warn.Printf("failed to read payload from client %s\n",
				conn.RemoteAddr())
			return
		}
		if header.Masked {
			ws.Cipher(buf[:n], header.Mask, 0)
		}

		// We expect we can now do the initial Bolt handshake
		magic, handshake := buf[:4], buf[4:20] // blaze it
		valid, err := bolt.ValidateMagic(magic)
		if !valid {
			warn.Fatal(err)
		}

		// negotiate client & server side bolt versions
		serverVersion := backend_server.Version().Bytes()
		clientVersion, err := bolt.ValidateHandshake(handshake, serverVersion)
		if err != nil {
			warn.Fatal(err)
		}

		// Complete Bolt handshake via WebSocket frame
		frame := ws.NewBinaryFrame(clientVersion)
		if err = ws.WriteFrame(conn, frame); err != nil {
			warn.Fatal(err)
		}
		info.Printf("Received2: %x", buf)

		// Let there be Bolt-via-WebSockets!
		info.Print("Bolt via websockets")
		handleBoltConn(bolt.NewWsConn(conn), clientVersion, backend_server)
	} else {
		// not bolt, not http...something else?
		info.Printf("client %s is speaking gibberish: %#v\n",
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
	info.Printf("Version: %v\n", clientVersion)
	info.Printf("Client: %v\n", client)
	select {
	case msg, ok := <-client.R():
		if !ok {
			warn.Println("failed to read expected Hello from client", msg, ok)
			return
		}
		hello = msg
	case <-time.After(30 * time.Second):
		warn.Println("timed out waiting for client to auth")
		return
	}
	logMessage("C->P", hello)

	if hello.T != bolt.HelloMsg {
		debug.Println("expected HelloMsg, got:", hello.T)
		return
	}
	debug.Println("expected HelloMsg, got:", hello.T)

	bolt_conn, err := back.InitBoltConnection(hello.Data, "tcp")
	if err != nil {
		warn.Println(err)
		return
	}

	// TODO: this seems odd...move parser and version stuff to bolt pkg
	v, _ := backend.ParseVersion(clientVersion)
	info.Printf("authenticated client %s speaking %s to %s server\n",
		client, v, back.MainInstance().Host)
	defer func() {
		info.Printf("goodbye to client %s\n", client)
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
	logMessage("P->C", &success_msg)
	err = client.WriteMessage(&success_msg)
	if err != nil {
		warn.Fatal(err)
	}

	// Time to begin the client-side event loop!
	startingTx := false
	manualTx := false
	halt := make(chan bool, 1)
	ack := make(chan bool, 1)

	var server bolt.BoltConn
	for {
		var msg *bolt.Message
		select {
		case m, ok := <-client.R():
			if ok {
				msg = m
				logMessage("C->P", msg)
			} else {
				debug.Println("potential client hangup")
				select {
				case halt <- true:
					debug.Println("client hangup, asking tx to halt")
				default:
					warn.Println("failed to send halt message to tx handler")
				}
				return
			}
		case <-time.After(time.Duration(MAX_IDLE_MINS) * time.Minute):
			warn.Println("client idle timeout")
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
		debug.Printf("The incoming client message %v is manual: %t and startingTx: %t", msg.T, manualTx, startingTx)
		if startingTx {
			mode, _ := bolt.ValidateMode(msg.Data)

			var n int
			if msg.T == bolt.BeginMsg {
				debug.Print("Debug begin MSG")
				_, _, err = bolt.ParseMap(msg.Data[4:])
				if err != nil {
					warn.Println(err)
					return
				}
			} else if msg.T == bolt.RunMsg {
				debug.Print("Debug begin RUN")
				pos := 4
				// query
				_, n, err = bolt.ParseString(msg.Data[pos:])
				if err != nil {
					warn.Println(err)
					return
				}
				pos = pos + n
				// query params
				_, n, err = bolt.ParseMap(msg.Data[pos:])
				if err != nil {
					warn.Println(err)
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
				warn.Printf("Couldn't find host")
			}

			if len(hosts) < 1 {
				warn.Println("No hosts")
				// TODO: return FailureMsg???
				return
			}
			host := hosts[0]

			// Are we already using a host? If so try to stop the
			// current tx handler before we create a new one
			if server != nil {
				select {
				case halt <- true:
					debug.Println("...asking current tx handler to halt")
					select {
					case <-ack:
						debug.Println("tx handler ack'd stop")
					case <-time.After(5 * time.Second):
						warn.Println("!!! timeout waiting for ack from tx handler")
					}
				default:
					// this shouldn't happen!
					panic("couldn't send halt to tx handler!")
				}
			}

			// Grab our host from our local pool
			// ok := false
			server = bolt_conn
			// if !ok {
			// 	warn.Println("no established connection for host", host)
			// 	return
			// }
			debug.Printf("grabbed conn for %s-access to db %s on host %s\n", mode, "ladida", host)

			// TODO: refactor channel handling...probably have handleTx() return new ones
			// instead of reusing the same ones. If we don't create new ones, there could
			// be lingering halt/ack messages. :-(
			halt = make(chan bool, 1)
			ack = make(chan bool, 1)

			// kick off a new tx handler routine
			go handleTx(client, bolt_conn, ack, halt)
			startingTx = false
		}

		// TODO: this connected/not-connected handling looks messy
		if server != nil {
			err = server.WriteMessage(msg)
			if err != nil {
				// TODO: figure out best way to handle failed writes
				panic(err)
			}
			logMessage("P->S", msg)
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
