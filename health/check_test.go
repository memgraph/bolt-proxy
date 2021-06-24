package health

import (
	"bytes"
	"net"
	"testing"
)

func TestHealthCheckHandler(t *testing.T) {
	left, right := net.Pipe()
	c := make(chan []byte)
	bad := []byte("GET /health HTTP/xxxx")
	ok := []byte("GET /health HTTP/1.1\r\n\r\n")

	go func() {
		for i := 0; i < 2; i++ {
			buf := make([]byte, 128)
			n, err := right.Read(buf)
			if err != nil {
				t.Fatal(err)
			}
			c <- buf[:n]
		}
	}()

	err := HandleHealthCheck(left, bad)
	if err == nil {
		t.Fatal("expected to fail with bad healthcheck request")
	}
	msg := <-c
	if !bytes.Equal([]byte(BAD_RESPONSE), msg) {
		t.Fatal("expected bad response to healthcheck request")
	}

	err = HandleHealthCheck(left, ok)
	if err != nil {
		t.Fatal(err)
	}
	msg = <-c
	if !bytes.Equal([]byte(OK_RESPONSE), msg) {
		t.Fatal("expected OK response to healthcheck request")
	}
}
