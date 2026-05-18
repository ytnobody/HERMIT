package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- assetName ---

func TestAssetName(t *testing.T) {
	name := assetName()
	want := fmt.Sprintf("hermit-%s-%s", runtime.GOOS, runtime.GOARCH)
	if name != want {
		t.Errorf("assetName() = %q, want %q", name, want)
	}
}

// --- findAsset ---

func TestFindAsset(t *testing.T) {
	tests := []struct {
		name   string
		assets []releaseAsset
		target string
		found  bool
	}{
		{
			name:   "found",
			assets: []releaseAsset{{Name: "hermit-linux-amd64", BrowserDownloadURL: "http://example.com/hermit-linux-amd64"}},
			target: "hermit-linux-amd64",
			found:  true,
		},
		{
			name:   "not found",
			assets: []releaseAsset{{Name: "hermit-darwin-arm64", BrowserDownloadURL: "http://example.com/hermit-darwin-arm64"}},
			target: "hermit-linux-amd64",
			found:  false,
		},
		{
			name:   "empty list",
			assets: []releaseAsset{},
			target: "hermit-linux-amd64",
			found:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findAsset(tc.assets, tc.target)
			if tc.found && got == nil {
				t.Error("expected asset, got nil")
			}
			if !tc.found && got != nil {
				t.Errorf("expected nil, got %+v", got)
			}
		})
	}
}

// --- verifyChecksum ---

func TestVerifyChecksum(t *testing.T) {
	data := []byte("hello hermit")
	sum := sha256.Sum256(data)
	validHex := hex.EncodeToString(sum[:])

	tests := []struct {
		name         string
		checksumData string
		assetName    string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "valid checksum",
			checksumData: validHex + "  hermit-linux-amd64\n",
			assetName:    "hermit-linux-amd64",
		},
		{
			name:         "valid checksum with ./ prefix",
			checksumData: validHex + "  ./hermit-linux-amd64\n",
			assetName:    "hermit-linux-amd64",
		},
		{
			name:         "wrong checksum",
			checksumData: "0000000000000000000000000000000000000000000000000000000000000000  hermit-linux-amd64\n",
			assetName:    "hermit-linux-amd64",
			wantErr:      true,
			errContains:  "checksum mismatch",
		},
		{
			name:         "asset not listed — skip",
			checksumData: validHex + "  hermit-darwin-arm64\n",
			assetName:    "hermit-linux-amd64",
		},
		{
			name:         "empty checksum file",
			checksumData: "",
			assetName:    "hermit-linux-amd64",
		},
		{
			name:         "malformed line ignored",
			checksumData: "onlyone\n" + validHex + "  hermit-linux-amd64\n",
			assetName:    "hermit-linux-amd64",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyChecksum(data, []byte(tc.checksumData), tc.assetName)
			if tc.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				} else if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("expected error containing %q, got %q", tc.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// --- installBinary ---

func TestInstallBinary_OK(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "hermit")
	data := []byte("fake binary content")

	if err := installBinary(data, destPath); err != nil {
		t.Fatalf("installBinary: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("installed content mismatch: got %q, want %q", got, data)
	}

	info, _ := os.Stat(destPath)
	if info.Mode()&0o111 == 0 {
		t.Error("installed binary should be executable")
	}
}

func TestInstallBinary_BadDir(t *testing.T) {
	err := installBinary([]byte("data"), "/nonexistent-dir/hermit")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}

// --- fetchLatestRelease ---

func TestFetchLatestRelease_OK(t *testing.T) {
	release := releaseInfo{
		TagName: "v1.0.0",
		Assets: []releaseAsset{
			{Name: "hermit-linux-amd64", BrowserDownloadURL: "http://example.com/hermit-linux-amd64"},
		},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer ts.Close()

	orig := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = orig })

	got, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TagName != "v1.0.0" {
		t.Errorf("expected v1.0.0, got %q", got.TagName)
	}
}

func TestFetchLatestRelease_404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	orig := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = orig })

	_, err := fetchLatestRelease()
	if err == nil {
		t.Error("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got %q", err.Error())
	}
}

func TestFetchLatestRelease_500(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	orig := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = orig })

	_, err := fetchLatestRelease()
	if err == nil {
		t.Error("expected error for 500")
	}
}

func TestFetchLatestRelease_BadJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "not json {{")
	}))
	defer ts.Close()

	orig := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = orig })

	_, err := fetchLatestRelease()
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestFetchLatestRelease_NetworkError(t *testing.T) {
	orig := githubReleasesAPI
	githubReleasesAPI = "http://127.0.0.1:1" // nothing listening
	t.Cleanup(func() { githubReleasesAPI = orig })

	_, err := fetchLatestRelease()
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- downloadBytes ---

func TestDownloadBytes_OK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "binary data")
	}))
	defer ts.Close()

	data, err := downloadBytes(ts.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(data) != "binary data" {
		t.Errorf("unexpected data: %q", data)
	}
}

func TestDownloadBytes_NonOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer ts.Close()

	_, err := downloadBytes(ts.URL)
	if err == nil {
		t.Error("expected error for non-200 status")
	}
}

func TestDownloadBytes_NetworkError(t *testing.T) {
	_, err := downloadBytes("http://127.0.0.1:1")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// --- cmdUpgrade ---

func TestCmdUpgrade_AlreadyUpToDate(t *testing.T) {
	orig := Version
	Version = "v1.0.0"
	t.Cleanup(func() { Version = orig })

	release := releaseInfo{TagName: "v1.0.0", Assets: []releaseAsset{}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer ts.Close()

	origAPI := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = origAPI })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdUpgrade()
	pw.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr)

	if !strings.Contains(buf.String(), "up to date") {
		t.Errorf("expected 'up to date', got %q", buf.String())
	}
}

func TestCmdUpgrade_NoAssetForPlatform(t *testing.T) {
	if os.Getenv("TEST_UPGRADE_NOASSET") != "" {
		release := releaseInfo{
			TagName: "v2.0.0",
			Assets:  []releaseAsset{{Name: "hermit-other-platform", BrowserDownloadURL: "http://example.com/other"}},
		}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(release)
		}))
		defer ts.Close()
		githubReleasesAPI = ts.URL
		Version = "dev"
		cmdUpgrade()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdUpgrade_NoAssetForPlatform", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_UPGRADE_NOASSET=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

func TestCmdUpgrade_APIError(t *testing.T) {
	if os.Getenv("TEST_UPGRADE_APIERR") != "" {
		githubReleasesAPI = "http://127.0.0.1:1"
		cmdUpgrade()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdUpgrade_APIError", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_UPGRADE_APIERR=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

func TestCmdUpgrade_ChecksumMismatch(t *testing.T) {
	if os.Getenv("TEST_UPGRADE_CSMISMATCH") != "" {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/release"):
				release := releaseInfo{
					TagName: "v2.0.0",
					Assets: []releaseAsset{
						{Name: assetName(), BrowserDownloadURL: "http://" + r.Host + "/binary"},
						{Name: "checksums.txt", BrowserDownloadURL: "http://" + r.Host + "/checksums"},
					},
				}
				json.NewEncoder(w).Encode(release)
			case strings.HasSuffix(r.URL.Path, "/binary"):
				w.Write([]byte("real binary"))
			case strings.HasSuffix(r.URL.Path, "/checksums"):
				fmt.Fprintf(w, "0000000000000000000000000000000000000000000000000000000000000000  %s\n", assetName())
			}
		}))
		defer ts.Close()
		githubReleasesAPI = ts.URL + "/release"
		Version = "dev"
		cmdUpgrade()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCmdUpgrade_ChecksumMismatch", "-test.v")
	cmd.Env = append(os.Environ(), "TEST_UPGRADE_CSMISMATCH=1")
	err := cmd.Run()
	if e, ok := err.(*exec.ExitError); ok && !e.Success() {
		return
	}
	t.Fatalf("expected non-zero exit, got: %v", err)
}

func TestCmdUpgrade_WithChecksum(t *testing.T) {
	dir := t.TempDir()
	fakeBinary := []byte("fake binary v2")
	sum := sha256.Sum256(fakeBinary)
	checksumContent := hex.EncodeToString(sum[:]) + "  " + assetName() + "\n"

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/release"):
			release := releaseInfo{
				TagName: "v2.0.0",
				Assets: []releaseAsset{
					{Name: assetName(), BrowserDownloadURL: "http://" + r.Host + "/binary"},
					{Name: "checksums.txt", BrowserDownloadURL: "http://" + r.Host + "/checksums"},
				},
			}
			json.NewEncoder(w).Encode(release)
		case strings.HasSuffix(r.URL.Path, "/binary"):
			w.Write(fakeBinary)
		case strings.HasSuffix(r.URL.Path, "/checksums"):
			fmt.Fprint(w, checksumContent)
		}
	}))
	defer ts.Close()

	origAPI := githubReleasesAPI
	githubReleasesAPI = ts.URL + "/release"
	t.Cleanup(func() { githubReleasesAPI = origAPI })

	destPath := filepath.Join(dir, "hermit")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	origBinPath := hermitBinaryPathFunc
	hermitBinaryPathFunc = func() (string, error) { return destPath, nil }
	t.Cleanup(func() { hermitBinaryPathFunc = origBinPath })

	orig := Version
	Version = "dev"
	t.Cleanup(func() { Version = orig })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdUpgrade()
	pw.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr)

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(got, fakeBinary) {
		t.Errorf("binary not updated: got %q", got)
	}
	if !strings.Contains(buf.String(), "v2.0.0") {
		t.Errorf("expected success message, got %q", buf.String())
	}
}

func TestCmdUpgrade_WithoutChecksum(t *testing.T) {
	dir := t.TempDir()
	fakeBinary := []byte("fake binary v3")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/release"):
			release := releaseInfo{
				TagName: "v3.0.0",
				Assets: []releaseAsset{
					{Name: assetName(), BrowserDownloadURL: "http://" + r.Host + "/binary"},
				},
			}
			json.NewEncoder(w).Encode(release)
		case strings.HasSuffix(r.URL.Path, "/binary"):
			w.Write(fakeBinary)
		}
	}))
	defer ts.Close()

	origAPI := githubReleasesAPI
	githubReleasesAPI = ts.URL + "/release"
	t.Cleanup(func() { githubReleasesAPI = origAPI })

	destPath := filepath.Join(dir, "hermit")
	if err := os.WriteFile(destPath, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	origBinPath := hermitBinaryPathFunc
	hermitBinaryPathFunc = func() (string, error) { return destPath, nil }
	t.Cleanup(func() { hermitBinaryPathFunc = origBinPath })

	orig := Version
	Version = "dev"
	t.Cleanup(func() { Version = orig })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdUpgrade()
	pw.Close()
	os.Stdout = origOut
	var buf bytes.Buffer
	buf.ReadFrom(pr)

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(got, fakeBinary) {
		t.Errorf("binary not updated: got %q", got)
	}
	if !strings.Contains(buf.String(), "v3.0.0") {
		t.Errorf("expected success message, got %q", buf.String())
	}
}

func TestMainSwitch_Upgrade(t *testing.T) {
	release := releaseInfo{TagName: "v1.0.0", Assets: []releaseAsset{}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(release)
	}))
	defer ts.Close()

	origAPI := githubReleasesAPI
	githubReleasesAPI = ts.URL
	t.Cleanup(func() { githubReleasesAPI = origAPI })

	orig := Version
	Version = "v1.0.0"
	t.Cleanup(func() { Version = orig })

	out := directMain(t, []string{"hermit", "upgrade"})
	if !strings.Contains(out, "up to date") {
		t.Errorf("expected 'up to date' via main upgrade, got %q", out)
	}
}
