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
