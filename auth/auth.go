package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

const loginURL = "https://www.indiegala.com/login_new/gcl"

type LoginResult struct {
	Success     bool
	StatusCode  int
	Cookies     []*http.Cookie
	RawResponse string // raw JSON (handy for debugging)
	Message     string // human readable reason
}

// Login performs authentication against the IndieGala API.
func Login(ctx context.Context, email, password string) (*http.Client, *LoginResult, error) {
	return loginWithURL(ctx, loginURL, email, password, true)
}

// loginWithURL is the internal login implementation that allows specifying a custom URL.
// saveSession controls whether to persist the session to disk.
func loginWithURL(ctx context.Context, targetURL, email, password string, saveSession bool) (*http.Client, *LoginResult, error) {
	jar, _ := cookiejar.New(nil)

	client := &http.Client{
		Jar:     jar,
		Timeout: 30 * time.Second,
	}

	form := url.Values{}
	form.Set("usre", email)
	form.Set("usrp", password)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("User-Agent", "galaClient")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	bodyStr := string(bodyBytes)

	var payload struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		// If JSON decoding fails, treat as hard error
		return nil, nil, fmt.Errorf("failed to decode login response: %w (body: %s)", err, bodyStr)
	}

	u, _ := url.Parse(targetURL)
	cookies := jar.Cookies(u)

	result := &LoginResult{
		StatusCode:  resp.StatusCode,
		Message:     payload.Message,
		Cookies:     cookies,
		RawResponse: bodyStr,
	}

	if resp.StatusCode == http.StatusOK && payload.Status == "success" {
		result.Success = true

		// Save session to disk (only in production, not tests)
		if saveSession {
		sess := &Session{Cookies: cookies}
		if err := SaveSession(sess); err != nil {
			return nil, nil, fmt.Errorf("login succeeded but saving session failed: %w", err)
			}
		}

		return client, result, nil
	}

	// Login failed
	return client, result, fmt.Errorf("login failed: %s (%s)", payload.Message, payload.Status)
}
