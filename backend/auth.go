package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

type Authenticator interface {
	Authenticate(user, token string) (bool, error)
}

type BasicAzureAuth struct {
	url   string
	group string
}

type azureDevOpsResponse struct {
	Count int `json:"count"`
	Value []struct {
		SubjectKind   string `json:"subjectKind"`
		Description   string `json:"description"`
		Domain        string `json:"domain"`
		PrincipalName string `json:"principalName"`
		MailAddress   string `json:"mailAddress"`
		Origin        string `json:"origin"`
		OriginID      string `json:"originId"`
		DisplayName   string `json:"displayName"`
		Links         struct {
			Self struct {
				Href string `json:"href"`
			} `json:"self"`
			Memberships struct {
				Href string `json:"href"`
			} `json:"memberships"`
			MembershipState struct {
				Href string `json:"href"`
			} `json:"membershipState"`
			StorageKey struct {
				Href string `json:"href"`
			} `json:"storageKey"`
		} `json:"_links"`
		URL            string `json:"url"`
		Descriptor     string `json:"descriptor"`
		IsCrossProject bool   `json:"isCrossProject,omitempty"`
	} `json:"value"`
}

func getAzureDevOpsResponse(body []byte) (*azureDevOpsResponse, error) {
	var a = new(azureDevOpsResponse)
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
		return &BasicAzureAuth{
			url:   authURL,
			group: authGroup,
		}, nil
	default:
		return nil, errors.New("no auth method found")
	}
}

func (basicAuth *BasicAzureAuth) Authenticate(user, token string) (bool, error) {
	var url string = fmt.Sprintf(basicAuth.url+"%s/_apis/graph/groups?api-version=5.1-preview.1", user)
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(user, token)
	rawResp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
		return false, err
	}

	bodyText, err := ioutil.ReadAll(rawResp.Body)
	if err != nil {
		log.Fatal(err)
		return false, err
	}
	if rawResp.StatusCode != 200 {
		return false, fmt.Errorf("unauthorized token")
	}

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

	return false, fmt.Errorf("user not authorized in %v group", basicAuth.group)
}
