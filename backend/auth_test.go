package backend

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBasicAzureAuth(t *testing.T) {
	const (
		user  string = "user"
		creds string = "creds"
	)
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		username, credentials, ok := r.BasicAuth()
		if !ok {
			t.Fatalf("auth ok %v", ok)
		}
		if username != user || credentials != creds {
			t.Fatalf("given user and creds do not match: %v, %v", username, credentials)
		}
		rw.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	basicAzureAuth := BasicAzureAuth{
		url:       ts.URL,
		group:     "",
		authorize: false,
	}
	auth, err := basicAzureAuth.Authenticate(user, creds)
	if err != nil || !auth {
		t.Fatalf("user not authenticated: %v", err)
	}
}

func TestBasicAzureAuthWrongCreds(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	basicAzureAuth := BasicAzureAuth{
		url:       ts.URL,
		group:     "",
		authorize: false,
	}
	auth, err := basicAzureAuth.Authenticate("user", "creds")
	if err == nil {
		t.Fatalf("expecting err msg")
	}
	if auth {
		t.Fatalf("user should not be authenticated")
	}
}

func TestBasicAzureAuthWithAuthorization(t *testing.T) {
	const (
		user  string = "user"
		creds string = "creds"
		group string = "group1"
	)
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		username, credentials, ok := r.BasicAuth()
		if !ok {
			t.Fatalf("auth ok %v", ok)
		}
		if username != user || credentials != creds {
			t.Fatalf("given user and creds do not match: %v, %v", username, credentials)
		}
		js, err := json.Marshal(AzureDevOpsResponse{
			Count: 1,
			Value: []AzureGroupValue{
				{
					DisplayName: group,
				},
			},
		})
		if err != nil {
			t.Fatalf("nt able to parse json: %v", err)
		}

		rw.WriteHeader(http.StatusOK)
		rw.Header().Set("Content-Type", "application/json")
		rw.Write(js)
	}))
	defer ts.Close()

	basicAzureAuth := BasicAzureAuth{
		url:       ts.URL,
		group:     "group1",
		authorize: true,
	}
	auth, err := basicAzureAuth.Authenticate(user, creds)
	if err != nil || !auth {
		t.Fatalf("user not authorized or authenticated: %v", err)
	}
}
