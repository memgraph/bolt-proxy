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
