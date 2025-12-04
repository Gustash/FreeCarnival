package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin_Success(t *testing.T) {
	// Create a mock server that returns a successful login response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and content type
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("expected Content-Type application/x-www-form-urlencoded, got %s", ct)
		}
		if ua := r.Header.Get("User-Agent"); ua != "galaClient" {
			t.Errorf("expected User-Agent galaClient, got %s", ua)
		}

		// Parse form data
		if err := r.ParseForm(); err != nil {
			t.Errorf("failed to parse form: %v", err)
		}
		if email := r.FormValue("usre"); email != "test@example.com" {
			t.Errorf("expected email test@example.com, got %s", email)
		}
		if pass := r.FormValue("usrp"); pass != "password123" {
			t.Errorf("expected password password123, got %s", pass)
		}

		// Set a test cookie
		http.SetCookie(w, &http.Cookie{
			Name:  "auth_token",
			Value: "test_token_123",
			Path:  "/",
		})

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Login successful",
		})
	}))
	defer server.Close()

	// Test with the mock server (saveSession=false for tests)
	_, result, err := loginWithURL(context.Background(), server.URL, "test@example.com", "password123", false)

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}
	if len(result.Cookies) == 0 {
		t.Error("expected cookies to be set")
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid email or password",
		})
	}))
	defer server.Close()

	_, result, err := loginWithURL(context.Background(), server.URL, "bad@example.com", "wrongpassword", false)

	if err == nil {
		t.Fatal("expected error for invalid credentials")
	}
	if result == nil {
		t.Fatal("expected result even on failure")
	}
	if result.Success {
		t.Error("expected Success to be false")
	}
	if result.Message != "Invalid email or password" {
		t.Errorf("expected message 'Invalid email or password', got %q", result.Message)
	}
}

func TestLogin_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Internal server error",
		})
	}))
	defer server.Close()

	_, result, err := loginWithURL(context.Background(), server.URL, "test@example.com", "password123", false)

	if err == nil {
		t.Fatal("expected error for server error")
	}
	if result == nil {
		t.Fatal("expected result even on server error")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", result.StatusCode)
	}
}

func TestLogin_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	_, _, err := loginWithURL(context.Background(), server.URL, "test@example.com", "password123", false)

	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
}

func TestLogin_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This handler would normally respond, but context should cancel first
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "success",
			"message": "Login successful",
		})
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, _, err := loginWithURL(ctx, server.URL, "test@example.com", "password123", false)

	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

