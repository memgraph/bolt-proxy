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

package frontend

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
		_, _ = conn.Write([]byte(BAD_RESPONSE))
		return errors.New("malformed http health check request")
	}

	// TODO: eventually we should check things are working right, but for
	// now, just consider it a liveness check.
	_, err = conn.Write([]byte(OK_RESPONSE))

	return err
}
