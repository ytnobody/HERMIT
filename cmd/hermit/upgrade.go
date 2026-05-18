package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var githubReleasesAPI = "https://api.github.com/repos/ytnobody/HERMIT/releases/latest"

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

func fetchLatestRelease() (*releaseInfo, error) {
	req, err := http.NewRequest(http.MethodGet, githubReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach GitHub API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found (404): hermit has not published any releases yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var info releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to parse release info: %w", err)
	}
	return &info, nil
}

func assetName() string {
	return fmt.Sprintf("hermit-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func findAsset(assets []releaseAsset, name string) *releaseAsset {
	for i := range assets {
		if assets[i].Name == name {
			return &assets[i]
		}
	}
	return nil
}

func downloadBytes(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:noctx
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func verifyChecksum(data []byte, checksumFileData []byte, assetName string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	for _, line := range strings.Split(string(checksumFileData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		want, name := parts[0], parts[1]
		// checksum files may list the bare filename or path like "./hermit-linux-amd64"
		name = strings.TrimPrefix(name, "./")
		if name == assetName {
			if got != want {
				return fmt.Errorf("checksum mismatch: got %s, want %s", got, want)
			}
			return nil
		}
	}
	// Asset name not found in the checksum file — treat as no checksum available.
	return nil
}

func installBinary(data []byte, destPath string) error {
	dir := filepath.Dir(destPath)
	tmp, err := os.CreateTemp(dir, "hermit-upgrade-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write failed: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("chmod failed: %w", err)
	}
	tmp.Close()

	if err := os.Rename(tmpName, destPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename failed: %w", err)
	}
	return nil
}

var hermitBinaryPathFunc = defaultHermitBinaryPath

func defaultHermitBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return execPath, nil
	}
	return resolved, nil
}

func cmdUpgrade() {
	fmt.Println("Checking for latest release...")

	release, err := fetchLatestRelease()
	if err != nil {
		fatal(err.Error())
	}

	fmt.Printf("Latest version: %s\n", release.TagName)
	if release.TagName == Version && Version != "dev" {
		fmt.Println("Already up to date.")
		return
	}

	name := assetName()
	asset := findAsset(release.Assets, name)
	if asset == nil {
		fatal(fmt.Sprintf("no binary asset found for %s in release %s", name, release.TagName))
		return
	}

	fmt.Printf("Downloading %s...\n", asset.BrowserDownloadURL)
	data, err := downloadBytes(asset.BrowserDownloadURL)
	if err != nil {
		fatal("download failed: " + err.Error())
	}

	// Optional checksum verification.
	checksumAsset := findAsset(release.Assets, "checksums.txt")
	if checksumAsset != nil {
		fmt.Println("Verifying checksum...")
		csData, err := downloadBytes(checksumAsset.BrowserDownloadURL)
		if err != nil {
			fatal("checksum download failed: " + err.Error())
		}
		if err := verifyChecksum(data, csData, name); err != nil {
			fatal(err.Error())
		}
	}

	destPath, err := hermitBinaryPathFunc()
	if err != nil {
		fatal("cannot determine hermit binary path: " + err.Error())
	}

	fmt.Printf("Installing to %s...\n", destPath)
	if err := installBinary(data, destPath); err != nil {
		fatal(err.Error())
	}

	fmt.Printf("Successfully upgraded to %s.\n", release.TagName)
}
