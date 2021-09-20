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
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAuth(t *testing.T) {
	authData := map[string]interface{}{
		"principal":   "user",
		"credentials": "creds",
	}
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		username, credentials, ok := r.BasicAuth()
		if !ok {
			t.Fatalf("auth ok %v", ok)
		}
		if username != authData["principal"] || credentials != authData["credentials"] {
			t.Fatalf("given user and creds do not match: %v, %v", username, credentials)
		}
		rw.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	basicAuth := BasicAuth{
		url: ts.URL,
	}
	err := basicAuth.Authenticate(authData)
	if err != nil {
		t.Fatalf("user not authenticated: %v", err)
	}
}

func TestBasicAuthWrongCreds(t *testing.T) {
	authData := map[string]interface{}{
		"principal":   "user",
		"credentials": "creds",
	}
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	basicAuth := BasicAuth{
		url: ts.URL,
	}
	err := basicAuth.Authenticate(authData)
	if err == nil {
		t.Fatalf("expecting err msg")
	}
}
