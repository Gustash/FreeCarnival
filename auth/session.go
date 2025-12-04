package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
)

const baseURL = "https://www.indiegala.com"

type Session struct {
	Cookies []*http.Cookie `json:"cookies"`
}

// testConfigDir can be set during tests to override the config directory
var testConfigDir string

// configDir returns something like ~/.config/FreeCarnival
func configDir() (string, error) {
	if testConfigDir != "" {
		return testConfigDir, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stdout, "Config dir: %s\n", dir)
	return filepath.Join(dir, "FreeCarnival"), nil
}

func sessionFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

func SaveSession(sess *Session) error {
	path, err := sessionFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
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
