package auth

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// setupTestConfigDir creates a temporary directory for tests and sets it as the config dir.
// Returns a cleanup function that should be deferred.
func setupTestConfigDir(t *testing.T) func() {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "freecarnival-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	testConfigDir = tmpDir
	return func() {
		testConfigDir = ""
		os.RemoveAll(tmpDir)
	}
}

func TestSaveSession(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	sess := &Session{
		Cookies: []*http.Cookie{
			{
				Name:   "auth_token",
				Value:  "test_token_123",
				Path:   "/",
				Domain: "indiegala.com",
			},
			{
				Name:   "session_id",
				Value:  "sess_456",
				Path:   "/",
				Domain: "indiegala.com",
			},
		},
	}

	err := SaveSession(sess)
	if err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(testConfigDir, "session.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("session.json was not created")
	}

	// Verify file permissions (should be 0600)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat session file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("expected file permissions 0600, got %o", perm)
	}
}

func TestLoadSessionClient_Success(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	// First save a session
	sess := &Session{
		Cookies: []*http.Cookie{
			{
				Name:   "auth_token",
				Value:  "test_token_123",
				Path:   "/",
				Domain: "indiegala.com",
			},
		},
	}
	if err := SaveSession(sess); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	// Now load it
	client, loadedSess, err := LoadSessionClient()
	if err != nil {
		t.Fatalf("LoadSessionClient failed: %v", err)
	}

	if client == nil {
		t.Fatal("expected client, got nil")
	}
	if loadedSess == nil {
		t.Fatal("expected session, got nil")
	}
	if len(loadedSess.Cookies) != 1 {
		t.Errorf("expected 1 cookie, got %d", len(loadedSess.Cookies))
	}
	if loadedSess.Cookies[0].Name != "auth_token" {
		t.Errorf("expected cookie name 'auth_token', got %q", loadedSess.Cookies[0].Name)
	}
	if loadedSess.Cookies[0].Value != "test_token_123" {
		t.Errorf("expected cookie value 'test_token_123', got %q", loadedSess.Cookies[0].Value)
	}

	// Verify client has a cookie jar
	if client.Jar == nil {
		t.Error("expected client to have a cookie jar")
	}
}

func TestLoadSessionClient_NoSession(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	// Try to load without saving first
	_, _, err := LoadSessionClient()
	if err == nil {
		t.Fatal("expected error when no session exists")
	}
	if err.Error() != "no saved session; please log in first" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestLoadSessionClient_InvalidJSON(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	// Create invalid JSON session file
	path := filepath.Join(testConfigDir, "session.json")
	if err := os.MkdirAll(testConfigDir, 0o700); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("not valid json"), 0o600); err != nil {
		t.Fatalf("failed to write invalid session file: %v", err)
	}

	_, _, err := LoadSessionClient()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSaveSession_MultipleTimes(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	// Save first session
	sess1 := &Session{
		Cookies: []*http.Cookie{
			{Name: "cookie1", Value: "value1"},
		},
	}
	if err := SaveSession(sess1); err != nil {
		t.Fatalf("first SaveSession failed: %v", err)
	}

	// Save second session (should overwrite)
	sess2 := &Session{
		Cookies: []*http.Cookie{
			{Name: "cookie2", Value: "value2"},
		},
	}
	if err := SaveSession(sess2); err != nil {
		t.Fatalf("second SaveSession failed: %v", err)
	}

	// Load and verify it's the second session
	_, loadedSess, err := LoadSessionClient()
	if err != nil {
		t.Fatalf("LoadSessionClient failed: %v", err)
	}

	if len(loadedSess.Cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(loadedSess.Cookies))
	}
	if loadedSess.Cookies[0].Name != "cookie2" {
		t.Errorf("expected cookie name 'cookie2', got %q", loadedSess.Cookies[0].Name)
	}
}

func TestSession_EmptyCookies(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	sess := &Session{
		Cookies: []*http.Cookie{},
	}
	if err := SaveSession(sess); err != nil {
		t.Fatalf("SaveSession failed: %v", err)
	}

	_, loadedSess, err := LoadSessionClient()
	if err != nil {
		t.Fatalf("LoadSessionClient failed: %v", err)
	}

	if len(loadedSess.Cookies) != 0 {
		t.Errorf("expected 0 cookies, got %d", len(loadedSess.Cookies))
	}
}

