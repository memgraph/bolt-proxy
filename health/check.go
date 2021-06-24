package health

import (
	"bufio"
	"bytes"
	"errors"
	"net"
	"net/http"
)

const (
	HEALTH_REQ   = "GET /health HTTP"
	OK_RESPONSE  = "HTTP/1.1 200 OK\r\n"
	BAD_RESPONSE = "HTTP/1.1 400 Bad Request\r\n"
)

// Check if the given buf looks like an HTTP GET to our /health endpoint
func IsHealthCheck(buf []byte) bool {
	return bytes.HasPrefix(buf, []byte(HEALTH_REQ))
}

// Given a connection client conn and its message as a byte-slice buf,
// validate it's an HTTP request. If so, write a "204 No Content" http
// response letting the caller know bolt-proxy is alive.
func HandleHealthCheck(conn net.Conn, buf []byte) error {
	reader := bytes.NewReader(buf)
	bufioReader := bufio.NewReader(reader)

	_, err := http.ReadRequest(bufioReader)
	if err != nil {
		conn.Write([]byte(BAD_RESPONSE))
		return errors.New("malformed http health check request")
	}

	// TODO: eventually we should check things are working right, but for
	// now, just consider it a liveness check.
	conn.Write([]byte(OK_RESPONSE))
	return nil
}
