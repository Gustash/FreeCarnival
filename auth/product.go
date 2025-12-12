package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gustash/freecarnival/logger"
)

const productInfoUrl = "https://developers.indiegala.com/get_product_info"

type ProductInfo struct {
	Status  string `json:"status"`
	Message string `json:"message"`

	GameDetails *GameDetails `json:"product_data,omitempty"`
}

type GameDetails struct {
	ExePath string `json:"exe_path,omitempty"`
	Args    string `json:"args,omitempty"`
	Cwd     string `json:"cwd,omitempty"`
}

// productInfoDir returns the directory for storing game details
func productInfoDir() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	productPath := filepath.Join(dir, "product")
	if err := os.MkdirAll(productPath, 0o700); err != nil {
		return "", err
	}
	return productPath, nil
}

// gameDetailsPath returns the file path to the specified game's details
func gameDetailsPath(slug string) (string, error) {
	dir, err := productInfoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.json", slug)), nil
}

// SaveGameDetails saves the provided GameDetails for the specified game slug to a file.
func SaveGameDetails(slug string, gameDetails *GameDetails) error {
	path, err := gameDetailsPath(slug)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(gameDetails, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// FetchGameDetails gets game details from the API (e.g. exe_name, args...).
func FetchGameDetails(ctx context.Context, client *http.Client, slug string) (*GameDetails, error) {
	return getProductInfoWithUrl(ctx, client, productInfoUrl, slug)
}

// getProductInfoWithUrl is an internal function that calls the server
// to fetch game details
func getProductInfoWithUrl(ctx context.Context, client *http.Client, targetURL, slug string) (*GameDetails, error) {
	products, err := LoadLibrary()
	if err != nil {
		return nil, err
	}
	product := FindProductBySlug(products, slug)
	if product == nil {
		return nil, fmt.Errorf("couldn't find %s in library", slug)
	}

	endpoint, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	queryParams := url.Values{}
	queryParams.Add("dev_id", product.Namespace)
	queryParams.Add("prod_name", slug)

	endpoint.RawQuery = queryParams.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "galaClient")
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var pi ProductInfo
	if err := json.Unmarshal(bodyBytes, &pi); err != nil {
		// If JSON decoding fails, treat as hard error
		bodyStr := string(bodyBytes)
		return nil, fmt.Errorf("failed to decode product info response: %w (body: %s)", err, bodyStr)
	}

	if resp.StatusCode == http.StatusOK && pi.Status == "success" {
		if err := SaveGameDetails(slug, pi.GameDetails); err != nil {
			logger.Warn("Failed to save product info", "slug", slug, "error", err)
		}

		return pi.GameDetails, nil
	}

	return nil, fmt.Errorf("fetching product info failed: %s (%s)", pi.Message, pi.Status)
}
