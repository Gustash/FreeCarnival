package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

const userInfoPath = "/login_new/user_info"

type BuildOS string

const (
	BuildOSWindows BuildOS = "win"
	BuildOSLinux   BuildOS = "lin"
	BuildOSMac     BuildOS = "mac"
)

type ProductVersion struct {
	Status  int     `json:"status"`  // u16 in Rust -> int in Go
	Enabled int     `json:"enabled"` // u8  in Rust -> int in Go
	Version string  `json:"version"`
	OS      BuildOS `json:"os"`
	// NaiveDateTime in Rust; we don't know the exact format, so keep as string for now
	Date string `json:"date"`
	Text string `json:"text"`
}

type ShowcaseContent struct {
	Content Content `json:"content"`
}

type Content struct {
	UserCollection []Product `json:"user_collection"`
}

type Product struct {
	Namespace   string           `json:"prod_dev_namespace"` // Rust: namespace, alias = "prod_dev_namespace"
	SluggedName string           `json:"prod_slugged_name"`  // Rust: slugged_name, alias = "prod_slugged_name"
	ID          uint64           `json:"id"`                 // Rust: id: u64
	Name        string           `json:"prod_name"`          // Rust: name, alias = "prod_name"
	IDKeyName   string           `json:"prod_id_key_name"`   // Rust: id_key_name, alias = "prod_id_key_name"
	Versions    []ProductVersion `json:"version"`            // Rust: version: Vec<ProductVersion>
}

type UserInfo struct {
	Status    string  `json:"status"`
	UserFound string  `json:"user_found"`
	Email     *string `json:"_indiegala_user_email,omitempty"`
	Username  *string `json:"_indiegala_username,omitempty"`
	UserID    *string `json:"_indiegala_user_id,omitempty"`

	ShowcaseContent *ShowcaseContent `json:"showcase_content,omitempty"`
}

type StoredUserInfo struct {
	Email    *string `json:"email,omitempty"`
	Username *string `json:"username,omitempty"`
	UserID   *string `json:"user_id,omitempty"`
}

func userFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "user.json"), nil
}

func libraryFilePath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "library.json"), nil
}

func SaveUserInfo(ui *StoredUserInfo) error {
	path, err := userFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(ui, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func SaveLibrary(products []Product) error {
	path, err := libraryFilePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(products, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// LoadLibrary loads the saved library from disk
func LoadLibrary() ([]Product, error) {
	path, err := libraryFilePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no saved library; please sync first")
		}
		return nil, err
	}

	var products []Product
	if err := json.Unmarshal(data, &products); err != nil {
		return nil, err
	}

	return products, nil
}

// FindProductBySlug finds a product in the library by its slugged name
func FindProductBySlug(products []Product, slug string) *Product {
	for i := range products {
		if products[i].SluggedName == slug {
			return &products[i]
		}
	}
	return nil
}

// FindProductVersion finds a specific version of a product
func (p *Product) FindVersion(version string, os BuildOS) *ProductVersion {
	for i := range p.Versions {
		if p.Versions[i].Version == version {
			if os == "" || p.Versions[i].OS == os {
				return &p.Versions[i]
			}
		}
	}
	return nil
}

// GetLatestVersion returns the latest version for the given OS (or current OS if not specified)
func (p *Product) GetLatestVersion(targetOS BuildOS) *ProductVersion {
	var latest *ProductVersion
	for i := range p.Versions {
		v := &p.Versions[i]
		if targetOS != "" && v.OS != targetOS {
			continue
		}
		if latest == nil || v.Date > latest.Date {
			latest = v
		}
	}
	return latest
}

// FetchUserInfo retrieves user information and library from the IndieGala API.
func FetchUserInfo(ctx context.Context, client *http.Client) (*StoredUserInfo, []Product, error) {
	url := baseURL + userInfoPath
	return fetchUserInfoWithURL(ctx, client, url)
}

// fetchUserInfoWithURL is the internal implementation that allows specifying a custom URL for testing.
func fetchUserInfoWithURL(ctx context.Context, client *http.Client, targetURL string) (*StoredUserInfo, []Product, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, nil, err
	}

	// Same UA as login
	req.Header.Set("User-Agent", "galaClient")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("user_info request failed with status %d", resp.StatusCode)
	}

	var ui UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&ui); err != nil {
		return nil, nil, fmt.Errorf("failed to decode user_info: %w", err)
	}

	// Mirror your Rust semantics: check `status` + `user_found`
	if ui.Status != "success" {
		return nil, nil, fmt.Errorf("user_info status=%q", ui.Status)
	}
	// In Rust it's a String; likely "true"/"false" – just expose it for now.
	if ui.UserFound == "false" {
		return nil, nil, fmt.Errorf("user not found (user_found=false)")
	}

	storedUi := StoredUserInfo{
		Email:    ui.Email,
		Username: ui.Username,
		UserID:   ui.UserID,
	}

	// Pull the library out (if present)
	var products []Product
	if ui.ShowcaseContent != nil {
		products = ui.ShowcaseContent.Content.UserCollection
	}

	return &storedUi, products, nil
}
