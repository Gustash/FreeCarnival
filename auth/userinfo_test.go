package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchUserInfo_Success(t *testing.T) {
	email := "test@example.com"
	username := "testuser"
	userID := "12345"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodGet {
			t.Errorf("expected GET method, got %s", r.Method)
		}
		if ua := r.Header.Get("User-Agent"); ua != "galaClient" {
			t.Errorf("expected User-Agent galaClient, got %s", ua)
		}

		response := UserInfo{
			Status:    "success",
			UserFound: "true",
			Email:     &email,
			Username:  &username,
			UserID:    &userID,
			ShowcaseContent: &ShowcaseContent{
				Content: Content{
					UserCollection: []Product{
						{
							ID:          1,
							Name:        "Test Game",
							Namespace:   "testdev",
							SluggedName: "test-game",
							IDKeyName:   "test_game_key",
							Versions: []ProductVersion{
								{
									Status:  1,
									Enabled: 1,
									Version: "1.0.0",
									OS:      BuildOSWindows,
									Date:    "2024-01-01",
									Text:    "Initial release",
								},
							},
						},
						{
							ID:          2,
							Name:        "Another Game",
							Namespace:   "anotherdev",
							SluggedName: "another-game",
							IDKeyName:   "another_game_key",
							Versions:    []ProductVersion{},
						},
					},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &http.Client{}
	ui, products, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err != nil {
		t.Fatalf("FetchUserInfo failed: %v", err)
	}

	if ui == nil {
		t.Fatal("expected user info, got nil")
	}
	if ui.Email == nil || *ui.Email != email {
		t.Errorf("expected email %q, got %v", email, ui.Email)
	}
	if ui.Username == nil || *ui.Username != username {
		t.Errorf("expected username %q, got %v", username, ui.Username)
	}
	if ui.UserID == nil || *ui.UserID != userID {
		t.Errorf("expected userID %q, got %v", userID, ui.UserID)
	}

	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}
	if products[0].Name != "Test Game" {
		t.Errorf("expected first product name 'Test Game', got %q", products[0].Name)
	}
	if products[1].Name != "Another Game" {
		t.Errorf("expected second product name 'Another Game', got %q", products[1].Name)
	}
}

func TestFetchUserInfo_UserNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := UserInfo{
			Status:    "success",
			UserFound: "false",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &http.Client{}
	_, _, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err == nil {
		t.Fatal("expected error for user not found")
	}
	if err.Error() != "user not found (user_found=false)" {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestFetchUserInfo_FailedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := UserInfo{
			Status:    "error",
			UserFound: "false",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &http.Client{}
	_, _, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err == nil {
		t.Fatal("expected error for failed status")
	}
}

func TestFetchUserInfo_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := &http.Client{}
	_, _, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err == nil {
		t.Fatal("expected error for HTTP error")
	}
}

func TestFetchUserInfo_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	client := &http.Client{}
	_, _, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchUserInfo_EmptyLibrary(t *testing.T) {
	email := "test@example.com"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := UserInfo{
			Status:    "success",
			UserFound: "true",
			Email:     &email,
			ShowcaseContent: &ShowcaseContent{
				Content: Content{
					UserCollection: []Product{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &http.Client{}
	ui, products, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err != nil {
		t.Fatalf("FetchUserInfo failed: %v", err)
	}
	if ui == nil {
		t.Fatal("expected user info, got nil")
	}
	if len(products) != 0 {
		t.Errorf("expected 0 products, got %d", len(products))
	}
}

func TestFetchUserInfo_NoShowcaseContent(t *testing.T) {
	email := "test@example.com"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := UserInfo{
			Status:          "success",
			UserFound:       "true",
			Email:           &email,
			ShowcaseContent: nil,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &http.Client{}
	ui, products, err := fetchUserInfoWithURL(context.Background(), client, server.URL)

	if err != nil {
		t.Fatalf("FetchUserInfo failed: %v", err)
	}
	if ui == nil {
		t.Fatal("expected user info, got nil")
	}
	if len(products) != 0 {
		t.Errorf("expected nil or empty products, got %d", len(products))
	}
}

func TestSaveUserInfo(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	email := "test@example.com"
	username := "testuser"
	userID := "12345"

	ui := &StoredUserInfo{
		Email:    &email,
		Username: &username,
		UserID:   &userID,
	}

	err := SaveUserInfo(ui)
	if err != nil {
		t.Fatalf("SaveUserInfo failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(testConfigDir, "user.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("user.json was not created")
	}

	// Verify file contents
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read user.json: %v", err)
	}

	var loadedUI StoredUserInfo
	if err := json.Unmarshal(data, &loadedUI); err != nil {
		t.Fatalf("failed to parse user.json: %v", err)
	}

	if loadedUI.Email == nil || *loadedUI.Email != email {
		t.Errorf("expected email %q, got %v", email, loadedUI.Email)
	}
}

func TestSaveUserInfo_NilFields(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	ui := &StoredUserInfo{
		Email:    nil,
		Username: nil,
		UserID:   nil,
	}

	err := SaveUserInfo(ui)
	if err != nil {
		t.Fatalf("SaveUserInfo failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(testConfigDir, "user.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read user.json: %v", err)
	}

	var loadedUI StoredUserInfo
	if err := json.Unmarshal(data, &loadedUI); err != nil {
		t.Fatalf("failed to parse user.json: %v", err)
	}

	if loadedUI.Email != nil || loadedUI.Username != nil || loadedUI.UserID != nil {
		t.Error("expected all fields to be nil")
	}
}

func TestSaveLibrary(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	products := []Product{
		{
			ID:          1,
			Name:        "Test Game",
			Namespace:   "testdev",
			SluggedName: "test-game",
			IDKeyName:   "test_game_key",
			Versions: []ProductVersion{
				{
					Status:  1,
					Enabled: 1,
					Version: "1.0.0",
					OS:      BuildOSWindows,
					Date:    "2024-01-01",
					Text:    "Initial release",
				},
			},
		},
		{
			ID:          2,
			Name:        "Linux Game",
			Namespace:   "linuxdev",
			SluggedName: "linux-game",
			IDKeyName:   "linux_game_key",
			Versions: []ProductVersion{
				{
					Status:  1,
					Enabled: 1,
					Version: "2.0.0",
					OS:      BuildOSLinux,
					Date:    "2024-02-01",
					Text:    "Linux release",
				},
			},
		},
	}

	err := SaveLibrary(products)
	if err != nil {
		t.Fatalf("SaveLibrary failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(testConfigDir, "library.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("library.json was not created")
	}

	// Verify file contents
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read library.json: %v", err)
	}

	var loadedProducts []Product
	if err := json.Unmarshal(data, &loadedProducts); err != nil {
		t.Fatalf("failed to parse library.json: %v", err)
	}

	if len(loadedProducts) != 2 {
		t.Fatalf("expected 2 products, got %d", len(loadedProducts))
	}
	if loadedProducts[0].Name != "Test Game" {
		t.Errorf("expected first product name 'Test Game', got %q", loadedProducts[0].Name)
	}
	if loadedProducts[1].Versions[0].OS != BuildOSLinux {
		t.Errorf("expected second product OS 'lin', got %q", loadedProducts[1].Versions[0].OS)
	}
}

func TestSaveLibrary_Empty(t *testing.T) {
	cleanup := setupTestConfigDir(t)
	defer cleanup()

	products := []Product{}

	err := SaveLibrary(products)
	if err != nil {
		t.Fatalf("SaveLibrary failed: %v", err)
	}

	// Verify file was created
	path := filepath.Join(testConfigDir, "library.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read library.json: %v", err)
	}

	var loadedProducts []Product
	if err := json.Unmarshal(data, &loadedProducts); err != nil {
		t.Fatalf("failed to parse library.json: %v", err)
	}

	if len(loadedProducts) != 0 {
		t.Errorf("expected 0 products, got %d", len(loadedProducts))
	}
}

func TestBuildOS_Constants(t *testing.T) {
	if BuildOSWindows != "win" {
		t.Errorf("expected BuildOSWindows to be 'win', got %q", BuildOSWindows)
	}
	if BuildOSLinux != "lin" {
		t.Errorf("expected BuildOSLinux to be 'lin', got %q", BuildOSLinux)
	}
	if BuildOSMac != "mac" {
		t.Errorf("expected BuildOSMac to be 'mac', got %q", BuildOSMac)
	}
}
