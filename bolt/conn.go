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

package bolt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/gobwas/ws"
)

// An abstraction of a Bolt-aware io.ReadWriterCloser. Allows for sending and
// receiving Messages, abstracting away the nuances of the transport.
//
// R() should simply be a means of accessing the channel containing any
// available Messages. (Might rename this later.) They should be handled async
// by the connection instance.
//
// WriteMessage() should synchronously try to write a Bolt Message to the
// connection.
type BoltConn interface {
	R() <-chan *Message
	WriteMessage(*Message) error
	io.Closer
}

// Designed for operating direct (e.g. TCP/IP-only) Bolt connections
type DirectConn struct {
	conn      io.ReadWriteCloser
	buf, temp []byte
	r         <-chan *Message
	chunking  bool
}

// Used for WebSocket-based Bolt connections
type WsConn struct {
	conn     io.ReadWriteCloser
	buf      []byte
	r        <-chan *Message
	chunking bool
}

var (
	BoltSignature = [...]byte{0x60, 0x60, 0xb0, 0x17}
	HttpSignature = [...]byte{0x47, 0x45, 0x54, 0x20}
)

// Create a new Direct Bolt Connection that uses simple Read/Write calls
// to transfer data.
//
// Note: the buffer size of 1024*128 is based off some manual testing as
// it was found 1024*64 was not enough, for instance, as some messages
// larger than 64kb have been encountered.
func NewDirectConn(c io.ReadWriteCloser) DirectConn {
	msgchan := make(chan *Message)
	dc := DirectConn{
		conn:     c,
		buf:      make([]byte, 1024*32),
		temp:     make([]byte, 2),
		r:        msgchan,
		chunking: false,
	}

	// Preset the buffer to be non-0x00, since zeros are
	// used as end-of-message markers
	for i := 0; i < len(dc.buf); i++ {
		dc.buf[i] = 0xff
	}

	// XXX: this design is ok for now, but in the event this go routine
	// aborts, it's not clear what the behavior will be of the BoltChan.
	go func() {
		for {
			message, err := dc.readMessage()
			if err != nil {
				if err == io.EOF {
					// log.Println("direct bolt connection hung-up")
					close(msgchan)
					return
				}
				// log.Printf("direct bolt connection disconnect: %s\n", err)
				return
			}
			msgchan <- message
		}
	}()

	return dc
}

func (c DirectConn) String() string {
	switch c.conn.(type) {
	case net.Conn:
		return fmt.Sprintf("Direct[%s]", c.conn.(net.Conn).RemoteAddr())
	default:
		return fmt.Sprintf("Direct[%s]", c.conn)
	}
}

func (c DirectConn) R() <-chan *Message {
	return c.r
}

// Read a single bolt Message, returning a point to it, or an error
//
// TODO: this needs a redesign...not sure best approach here, but
// maybe involving a BufferedReader so we can more easily peek at
// if we've got a chunked message or a full message. Then we could
// adapt this to how the WsConn version works, which returns 0 or
// many messages at once.
//
// Also, would be good to NOT DECHUNK like this currently does.
func (c *DirectConn) readMessage() (*Message, error) {
	var (
		t Type
	)

	underreads := 0
	pos := 0

	// TODO: abstract out a "readfully" like function...this under-read
	// handling logic is so error prone and ugly

	if !c.chunking {
		for pos < 2 {
			// We need to grab 2 bytes to get the message length
			n, err := c.conn.Read(c.buf[pos:2])
			if err != nil {
				return nil, err
			}
			// TODO: deal with this horrible issue!
			if n < 2 {
				underreads++
				if underreads > 5 {
					panic("under reads...something's up")
				}
			}
			pos = pos + n
		}
	} else {
		// We should have the msglen bytes in our temp buffer
		// from the last time we processed a message and found
		// we were chunking
		c.buf[0] = c.temp[0]
		c.buf[1] = c.temp[1]
		pos = 2
	}

	msglen := int(binary.BigEndian.Uint16(c.buf[:pos]))
	endOfData := pos + msglen

	// handle short reads of user data
	for pos < endOfData {
		n, err := c.conn.Read(c.buf[pos:endOfData])
		if err != nil {
			return nil, err
		}
		pos = pos + n
	}

	// read another 2 bytes...wasteful syscalls, but for now keep it simple
	// if the next 2 bytes are 0x00, 0x00 we're done the message, but
	// if the next 2 bytes are something else, we know we're chunking
	// this shouldn't block (for long) as if chunking there should always
	// be another message on the wire.
	endOfData = pos + 2
	underreads = 0
	for pos < endOfData {
		n, err := c.conn.Read(c.buf[pos:endOfData])
		if err != nil {
			return nil, err
		}
		if n < 2 {
			underreads++
			if underreads > 5 {
				panic("too many under reads!")
			}
		}
		pos = pos + n
	}

	// We can only inspect the type if we haven't yet been in chunking mode
	if !c.chunking {
		t = IdentifyType(c.buf[:pos])
	} else {
		t = ChunkedMsg
	}

	// Check last 2 bytes to see if we're starting or ending chunking
	if bytes.Equal([]byte{0x00, 0x00}, c.buf[pos-2:pos]) {
		c.chunking = false
		// scrub our temp buffer as we're done chunking
		c.temp[0] = 0xff
		c.temp[1] = 0xff
	} else {
		c.chunking = true
		// preserve these 2 bytes as they're the msglen of the next
		// chunked message coming on the wire
		c.temp[0] = c.buf[pos-2]
		c.temp[1] = c.buf[pos-1]
		// scrubbin' bubbles
		c.buf[pos-2] = 0xff
		c.buf[pos-1] = 0xff
		// backup 2 bytes...these aren't part of our message!
		pos = pos - 2
	}

	// Copy data into Message...
	data := make([]byte, pos)
	copy(data, c.buf[:pos])

	for i := 0; i < pos; i++ {
		c.buf[i] = 0xff
	}

	return &Message{
		T:    t,
		Data: data,
	}, nil
}

func (c DirectConn) WriteMessage(m *Message) error {
	// TODO validate message?

	n, err := c.conn.Write(m.Data)
	if err != nil {
		return err
	}
	if n != len(m.Data) {
		// TODO: loop to write all data?
		panic("incomplete message written")
	}

	return nil
}

func (c DirectConn) Close() error {
	return c.conn.Close()
}

func NewWsConn(c io.ReadWriteCloser) WsConn {
	msgchan := make(chan *Message)
	ws := WsConn{
		conn:     c,
		buf:      make([]byte, 1024*32),
		r:        msgchan,
		chunking: false,
	}

	// 0xff out the buffer
	for i := 0; i < len(ws.buf); i++ {
		ws.buf[i] = 0xff
	}

	go func() {
		for {
			messages, err := ws.readMessages()
			if err != nil {
				if err == io.EOF {
					// log.Println("bolt ws connection hung-up")
					close(msgchan)
					return
				}
				// log.Printf("ws bolt connection disconnect: %s\n", err)
				return
			}
			for _, message := range messages {
				if message == nil {
					panic("ws message = nil!")
				}
				msgchan <- message
			}
		}
	}()

	return ws
}

func (c WsConn) R() <-chan *Message {
	return c.r
}

func (c WsConn) String() string {
	switch c.conn.(type) {
	case net.Conn:
		return fmt.Sprintf("WebSocket[%s]", c.conn.(net.Conn).RemoteAddr())
	default:
		return fmt.Sprintf("WebSocket[%s]", c.conn)
	}
}

// Read 0 or many Bolt Messages from a WebSocket frame since, apparently,
// small Bolt Messages sometimes get packed into a single Frame(?!).
//
// For example, I've seen RUN + PULL all in 1 WebSocket frame.
func (c WsConn) readMessages() ([]*Message, error) {
	messages := make([]*Message, 0)

	header, err := ws.ReadHeader(c.conn)
	if err != nil {
		return nil, err
	}

	if !header.Fin {
		panic("unsupported header fin")
	}

	switch header.OpCode {
	case ws.OpClose:
		return nil, io.EOF
	case ws.OpPing, ws.OpPong, ws.OpContinuation, ws.OpText:
		panic(fmt.Sprintf("unsupported websocket opcode: %v\n", header.OpCode))
		// return nil, errors.New(msg)
	}

	// TODO: handle header.Length == 0 situations?
	if header.Length == 0 {
		return nil, errors.New("zero length header?!")
	}

	// TODO: under-reads!!!
	n, err := c.conn.Read(c.buf[:header.Length])
	if err != nil {
		return nil, err
	}

	if header.Masked {
		ws.Cipher(c.buf[:n], header.Mask, 0)
		header.Masked = false
	}

	// WebSocket frames might contain multiple bolt messages...oh, joy
	// XXX: for now we don't look for chunks across frame boundaries
	pos := 0

	for pos < int(header.Length) {
		msglen := int(binary.BigEndian.Uint16(c.buf[pos : pos+2]))

		// since we've already got the data in our buffer, we can
		// peek to see if we're about to or still chunking (or not)
		if bytes.Equal([]byte{0x0, 0x0}, c.buf[pos+msglen+2:pos+msglen+4]) {
			c.chunking = false
		} else {
			c.chunking = true
		}

		// we'll let the combination of the type and the chunking
		// flag dictate behavior as we're not cleaning our buffer
		// afterwards, so maaaaaybe there was a false positive
		sizeOfMsg := msglen + 4
		msgtype := IdentifyType(c.buf[pos:])
		if msgtype == UnknownMsg {
			msgtype = ChunkedMsg
		}
		if c.chunking {
			sizeOfMsg = msglen + 2
		}

		data := make([]byte, sizeOfMsg)
		copy(data, c.buf[pos:pos+sizeOfMsg])
		msg := Message{
			T:    msgtype,
			Data: data,
		}
		messages = append(messages, &msg)

		pos = pos + sizeOfMsg
	}

	// we need to 0xff out the buffer to prevent any secrets residing
	// in memory, but also so we don't get false 0x00 0x00 padding
	for i := 0; i < n; i++ {
		c.buf[i] = 0xff
	}

	return messages, nil
}
func (c WsConn) WriteMessage(m *Message) error {
	frame := ws.NewBinaryFrame(m.Data)
	err := ws.WriteFrame(c.conn, frame)

	return err
}

func (c WsConn) Close() error {
	return c.conn.Close()
}
