package bot

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type SirusResponse struct {
	Realms []Realm `json:"realms"`
}

type Realm struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	IsOnline bool   `json:"isOnline"`
}

func (b *Bot) CheckSirusUp() (string, bool) {
	var data SirusResponse
	sirusUrl := "https://sirus.su/api/statistic/tooltip.json"

	client := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MaxVersion: tls.VersionTLS12,
		}},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	req, err := http.NewRequest(http.MethodGet, sirusUrl, nil)

	if err != nil {
		log.Println("error making http request:", err)
	}

	req.Header.Set("User-Agent", "PostmanRuntime/7.43.0")

	res, err := client.Do(req)

	if err != nil {
		log.Println("error making http request:", err)
	}

	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)

	if err != nil {
		log.Println("error reading the request:", err)
		return "", false
	}

	if res.StatusCode != 200 {
		log.Println("status code not 200:", res.StatusCode)
		return "", false
	}

	if infoErr := json.Unmarshal(body, &data); infoErr != nil {
		return "", false
	}

	return data.Realms[1].Name, data.Realms[1].IsOnline
}
