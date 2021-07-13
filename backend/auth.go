package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"
)

type Authenticator interface {
	Authenticate(user, creds string) (bool, error)
}

type BasicAzureAuth struct {
	url       string
	group     string
	authorize bool
}

type AzureGroupValue struct {
	SubjectKind   string `json:"subjectKind",string,omitempty`
	Description   string `json:"description",string,omitempty`
	Domain        string `json:"domain",string,omitempty`
	PrincipalName string `json:"principalName",string,omitempty`
	MailAddress   string `json:"mailAddress",string,omitempty`
	Origin        string `json:"origin",string,omitempty`
	OriginID      string `json:"originId",string,omitempty`
	DisplayName   string `json:"displayName",string,omitempty`
}

type AzureDevOpsResponse struct {
	Count int               `json:"count"`
	Value []AzureGroupValue `json:"value"`
}

func getAzureDevOpsResponse(body []byte) (*AzureDevOpsResponse, error) {
	var a = new(AzureDevOpsResponse)
	err := json.Unmarshal(body, &a)
	if err != nil {
		fmt.Println("whoops:", err)
	}
	return a, err
}

func NewAuth() (Authenticator, error) {
	authMethod := os.Getenv("AUTH_METHOD")

	switch authMethod {
	case "BASIC_AUTH_AZURE":
		authURL := os.Getenv("AUTH_AZURE_URL")
		authGroup := os.Getenv("AUTH_AZURE_GROUP")
		authorize := false
		if authGroup != "" {
			authorize = true
		}

		return &BasicAzureAuth{
			url:       authURL,
			group:     authGroup,
			authorize: authorize,
		}, nil
	default:
		return nil, errors.New("no auth method found")
	}
}

func (basicAuth *BasicAzureAuth) Authenticate(user, creds string) (bool, error) {
	// var url string = fmt.Sprintf(basicAuth.url+"%s/_apis/graph/groups?api-version=5.1-preview.1", user)
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	req, err := http.NewRequest("GET", basicAuth.url, nil)
	if err != nil {
		return false, err
	}
	req.SetBasicAuth(user, creds)
	rawResp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	if rawResp.StatusCode != 200 {
		return false, errors.New("unauthorized creds")
	}

	bodyText, err := ioutil.ReadAll(rawResp.Body)
	if err != nil {
		return false, err
	}

	if basicAuth.authorize {
		resp, err := getAzureDevOpsResponse([]byte(bodyText))
		if err != nil {
			log.Fatal(err)
			return false, err
		}

		for _, v := range resp.Value {
			if v.DisplayName == basicAuth.group {
				return true, nil
			}
		}
	} else {
		return true, nil
	}

	return false, fmt.Errorf("user not authorized in %v group", basicAuth.group)
}
