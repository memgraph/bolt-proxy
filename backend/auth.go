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
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

type Authenticator interface {
	Authenticate(authData map[string]interface{}) error
}

type BasicAuth struct {
	url string
}

type AADTokenAuth struct {
	provider string
	clientID string
}

func NewAuth() (Authenticator, error) {
	authMethod := os.Getenv("AUTH_METHOD")

	switch authMethod {
	case "BASIC_AUTH":
		authURL := os.Getenv("BASIC_AUTH_URL")
		if authURL == "" {
			return nil, errors.New("BASIC_AUTH_URL must be set when using BASIC_AUTH")
		}

		return &BasicAuth{
			url: authURL,
		}, nil
	case "AAD_TOKEN_AUTH":
		clientID := os.Getenv("AAD_TOKEN_CLIENT_ID")
		provider := os.Getenv("AAD_TOKEN_PROVIDER")
		if clientID == "" || provider == "" {
			return nil, errors.New("AAD_TOKEN_CLIENT_ID and AAD_TOKEN_PROVIDER must be set when using AAD_TOKEN_AUTH")
		}

		return &AADTokenAuth{
			provider: provider,
			clientID: clientID,
		}, nil
	default:
		return nil, nil
	}
}

func (auth *BasicAuth) Authenticate(authData map[string]interface{}) error {
	principal, creds, err := getCredentials(authData)
	if err != nil {
		return err
	}

	client := &http.Client{
		Timeout: time.Second * 5,
	}
	req, err := http.NewRequest("GET", auth.url, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(principal, creds)
	rawResp, err := client.Do(req)
	if err != nil {
		return err
	}
	if rawResp.StatusCode != 200 {
		return errors.New("unauthorized creds")
	}

	return nil
}

func (auth *AADTokenAuth) Authenticate(authData map[string]interface{}) error {
	_, jwtString, err := getCredentials(authData)
	if err != nil {
		return err
	}

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, auth.provider)
	if err != nil {
		return err
	}

	var verifier = provider.Verifier(&oidc.Config{ClientID: auth.clientID})

	// Parse and verify ID Token payload.
	_, err = verifier.Verify(ctx, jwtString)
	if err != nil {
		return err
	}
	return nil
}

func getCredentials(authData map[string]interface{}) (string, string, error) {
	principal, ok := authData["principal"].(string)
	if !ok {
		return "", "", errors.New("no principal")
	}
	creds, ok := authData["credentials"].(string)
	if !ok {
		return "", "", errors.New("no credentials")
	}

	return principal, creds, nil
}
