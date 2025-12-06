package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
)

const baseURL = "https://www.indiegala.com"

// Session holds authentication cookies for IndieGala.
type Session struct {
	Cookies []*http.Cookie `json:"cookies"`
}

func sessionFilePath() (string, error) {
	return configFile("session.json")
}

func SaveSession(sess *Session) error {
	path, err := sessionFilePath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// ClearSession removes the saved session file, effectively logging out.
func ClearSession() error {
	path, err := sessionFilePath()
	if err != nil {
		return err
	}

	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil // Already logged out
	}
	return err
}

// LoadSessionClient loads the saved cookies and returns a ready-to-use HTTP client.
func LoadSessionClient() (*http.Client, *Session, error) {
	path, err := sessionFilePath()
	if err != nil {
		return nil, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("no saved session; please log in first")
		}
		return nil, nil, err
	}

	var sess Session
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, nil, err
	}

	u, _ := url.Parse(baseURL)
	jar.SetCookies(u, sess.Cookies)

	client := &http.Client{
		Jar: jar,
	}

	return client, &sess, nil
}
