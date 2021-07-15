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

		return &BasicAuth{
			url: authURL,
		}, nil
	case "AAD_TOKEN_AUTH":
		clientID := os.Getenv("AAD_TOKEN_CLIENT_ID")
		provider := os.Getenv("AAD_TOKEN_PROVIDER")

		return &AADTokenAuth{
			provider: provider,
			clientID: clientID,
		}, nil
	default:
		return nil, errors.New("no auth method found")
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
	jwtString, _, err := getCredentials(authData)
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
