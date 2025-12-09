package download

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gustash/freecarnival/auth"
)

func TestChunkDownloader_Download(t *testing.T) {
	chunkData := []byte("test chunk data for downloading")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Header.Get("User-Agent") != "galaClient" {
			t.Errorf("expected User-Agent galaClient, got %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write(chunkData)
	}))
	defer server.Close()

	product := &auth.Product{
		Namespace: "test",
		IDKeyName: "game",
	}

	downloader := NewChunkDownloader(&http.Client{}, product, auth.BuildOSWindows, nil)

	// Test doDownload directly with our test server URL
	data, err := downloader.doDownload(context.Background(), server.URL)
	if err != nil {
		t.Fatalf("doDownload failed: %v", err)
	}

	if !bytes.Equal(data, chunkData) {
		t.Errorf("downloaded data = %q, expected %q", string(data), string(chunkData))
	}

	if requestCount != 1 {
		t.Errorf("expected 1 HTTP request, got %d", requestCount)
	}
}

func TestChunkDownloader_HTTPError(t *testing.T) {
	attemptCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	product := &auth.Product{
		Namespace: "test",
		IDKeyName: "game",
	}

	downloader := NewChunkDownloader(&http.Client{}, product, auth.BuildOSWindows, nil)

	_, err := downloader.doDownload(context.Background(), server.URL)
	if err == nil {
		t.Error("expected error on 500 response")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Errorf("expected HTTPError, got %T", err)
	}

	if httpErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", httpErr.StatusCode)
	}

	if attemptCount != 1 {
		t.Errorf("expected 1 attempt, got %d", attemptCount)
	}
}
