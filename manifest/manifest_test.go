package manifest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gustash/freecarnival/auth"
)

func TestParseBuildManifest(t *testing.T) {
	csvData := `Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag
1048576,1,abc123,0,test/file.txt,tag1
0,0,,40,test/directory,
512,1,def456,0,test/small.dat,tag2
`
	records, err := parseBuildManifest([]byte(csvData))
	if err != nil {
		t.Fatalf("parseBuildManifest failed: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	if records[0].SizeInBytes != 1048576 {
		t.Errorf("record[0].SizeInBytes = %d, expected 1048576", records[0].SizeInBytes)
	}
	if records[0].Chunks != 1 {
		t.Errorf("record[0].Chunks = %d, expected 1", records[0].Chunks)
	}
	if records[0].SHA != "abc123" {
		t.Errorf("record[0].SHA = %q, expected %q", records[0].SHA, "abc123")
	}
	if records[0].Flags != 0 {
		t.Errorf("record[0].Flags = %d, expected 0", records[0].Flags)
	}
	if records[0].IsDirectory() {
		t.Error("record[0] should not be a directory")
	}

	if records[1].Flags != 40 {
		t.Errorf("record[1].Flags = %d, expected 40", records[1].Flags)
	}
	if !records[1].IsDirectory() {
		t.Error("record[1] should be a directory")
	}
	if !records[1].IsEmpty() {
		t.Error("record[1] should be empty")
	}

	if records[2].SizeInBytes != 512 {
		t.Errorf("record[2].SizeInBytes = %d, expected 512", records[2].SizeInBytes)
	}
}

func TestParseBuildManifest_EmptyCSV(t *testing.T) {
	_, err := parseBuildManifest([]byte(""))
	if err == nil {
		t.Error("expected error for empty CSV")
	}
}

func TestParseBuildManifest_HeaderOnly(t *testing.T) {
	csvData := `Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag
`
	records, err := parseBuildManifest([]byte(csvData))
	if err != nil {
		t.Fatalf("parseBuildManifest failed: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records, got %d", len(records))
	}
}

func TestParseChunksManifest(t *testing.T) {
	csvData := `ID,Filepath,Chunk SHA
0,test/file.txt,chunk_0_abc123def456
1,test/file.txt,chunk_1_789xyz000111
0,test/small.dat,single_0_aabbccdd
`
	records, err := parseChunksManifest([]byte(csvData))
	if err != nil {
		t.Fatalf("parseChunksManifest failed: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 records, got %d", len(records))
	}

	if records[0].ID != 0 {
		t.Errorf("record[0].ID = %d, expected 0", records[0].ID)
	}
	if records[0].ChunkSHA != "chunk_0_abc123def456" {
		t.Errorf("record[0].ChunkSHA = %q, expected %q", records[0].ChunkSHA, "chunk_0_abc123def456")
	}

	if records[1].ID != 1 {
		t.Errorf("record[1].ID = %d, expected 1", records[1].ID)
	}
}

func TestGetChunkURL(t *testing.T) {
	product := &auth.Product{
		Namespace: "testdev",
		IDKeyName: "test-game-uuid",
	}

	url := GetChunkURL(product, auth.BuildOSWindows, "chunk_sha_value")

	expected := ContentURL + "/DevShowCaseSourceVolume/dev_fold_testdev/test-game-uuid/win/chunk_sha_value"
	if url != expected {
		t.Errorf("GetChunkURL() = %q, expected %q", url, expected)
	}
}

func TestGetChunkURL_DifferentOS(t *testing.T) {
	product := &auth.Product{
		Namespace: "dev",
		IDKeyName: "game",
	}

	tests := []struct {
		os       auth.BuildOS
		expected string
	}{
		{auth.BuildOSWindows, ContentURL + "/DevShowCaseSourceVolume/dev_fold_dev/game/win/sha"},
		{auth.BuildOSLinux, ContentURL + "/DevShowCaseSourceVolume/dev_fold_dev/game/lin/sha"},
		{auth.BuildOSMac, ContentURL + "/DevShowCaseSourceVolume/dev_fold_dev/game/mac/sha"},
	}

	for _, tt := range tests {
		t.Run(string(tt.os), func(t *testing.T) {
			url := GetChunkURL(product, tt.os, "sha")
			if url != tt.expected {
				t.Errorf("GetChunkURL() = %q, expected %q", url, tt.expected)
			}
		})
	}
}

func TestLatin1ToUTF8(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"ASCII only", "hello world", "hello world"},
		{"empty string", "", ""},
		{"Latin-1 é (0xe9)", string([]byte{0xe9}), "é"},
		{"Latin-1 ñ (0xf1)", string([]byte{0xf1}), "ñ"},
		{"Latin-1 ü (0xfc)", string([]byte{0xfc}), "ü"},
		{"Mixed ASCII and Latin-1", "caf" + string([]byte{0xe9}), "café"},
		{"Path with Latin-1", "Mus" + string([]byte{0xe9}) + "e/file.txt", "Musée/file.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := latin1ToUTF8(tt.input)
			if result != tt.expected {
				t.Errorf("latin1ToUTF8(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Unix path", "path/to/file", "path" + string(filepath.Separator) + "to" + string(filepath.Separator) + "file"},
		{"Windows backslashes", `path\to\file`, "path" + string(filepath.Separator) + "to" + string(filepath.Separator) + "file"},
		{"Mixed separators", `path\to/file`, "path" + string(filepath.Separator) + "to" + string(filepath.Separator) + "file"},
		{"Single filename", "file.txt", "file.txt"},
		{"Empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePath(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePath(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizePath_PreservesStructure(t *testing.T) {
	input := `Game\Data\Levels\level1.dat`
	result := normalizePath(input)

	if runtime.GOOS == "windows" {
		if result != `Game\Data\Levels\level1.dat` {
			t.Errorf("normalizePath(%q) = %q, expected Windows path", input, result)
		}
	} else {
		if result != "Game/Data/Levels/level1.dat" {
			t.Errorf("normalizePath(%q) = %q, expected Unix path", input, result)
		}
	}
}

func TestFetchCSV_Success(t *testing.T) {
	csvData := `Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag
1000,1,sha123,0,test.txt,tag
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "galaClient" {
			t.Errorf("expected User-Agent galaClient, got %q", r.Header.Get("User-Agent"))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(csvData))
	}))
	defer server.Close()

	client := &http.Client{}
	data, err := fetchCSV(context.Background(), client, server.URL)
	if err != nil {
		t.Fatalf("fetchCSV failed: %v", err)
	}

	if string(data) != csvData {
		t.Errorf("fetchCSV returned unexpected data")
	}
}

func TestFetchCSV_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &http.Client{}
	_, err := fetchCSV(context.Background(), client, server.URL)
	if err == nil {
		t.Error("expected error for HTTP 404")
	}
}

func TestFetchCSV_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {}
	}))
	defer server.Close()

	client := &http.Client{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := fetchCSV(ctx, client, server.URL)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestParseBuildManifest_WithLatin1Filenames(t *testing.T) {
	csvData := "Size in Bytes,Chunks,SHA,Flags,File Name,Change Tag\n" +
		"1000,1,sha,0,Mus" + string([]byte{0xe9}) + "e/fichier.txt,tag\n"

	records, err := parseBuildManifest([]byte(csvData))
	if err != nil {
		t.Fatalf("parseBuildManifest failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	expectedPath := normalizePath("Musée/fichier.txt")
	if records[0].FileName != expectedPath {
		t.Errorf("FileName = %q, expected %q", records[0].FileName, expectedPath)
	}
}

func TestParseChunksManifest_WithBackslashPaths(t *testing.T) {
	csvData := `ID,Filepath,Chunk SHA
0,Game\Data\file.txt,chunk_sha
`
	records, err := parseChunksManifest([]byte(csvData))
	if err != nil {
		t.Fatalf("parseChunksManifest failed: %v", err)
	}

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	expectedPath := normalizePath("Game/Data/file.txt")
	if records[0].FilePath != expectedPath {
		t.Errorf("FilePath = %q, expected %q", records[0].FilePath, expectedPath)
	}
}

func TestExtractSHA(t *testing.T) {
	tests := []struct {
		name     string
		chunkID  string
		expected string
	}{
		{"full format", "prefix_0_sha256hash", "sha256hash"},
		{"no underscore", "sha256hash", "sha256hash"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractSHA(tt.chunkID)
			if result != tt.expected {
				t.Errorf("ExtractSHA(%q) = %q, expected %q", tt.chunkID, result, tt.expected)
			}
		})
	}
}

func TestBuildRecord_IsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		flags    int
		expected bool
	}{
		{"directory flag 40", 40, true},
		{"file flag 0", 0, false},
		{"other flag 1", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &BuildRecord{Flags: tt.flags}
			if got := r.IsDirectory(); got != tt.expected {
				t.Errorf("IsDirectory() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestBuildRecord_IsEmpty(t *testing.T) {
	tests := []struct {
		name        string
		sizeInBytes int
		expected    bool
	}{
		{"empty file", 0, true},
		{"non-empty file", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &BuildRecord{SizeInBytes: tt.sizeInBytes}
			if got := r.IsEmpty(); got != tt.expected {
				t.Errorf("IsEmpty() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

