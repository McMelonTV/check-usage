package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func buildTestTarGz(t *testing.T, files map[string][]byte, symlinks map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o755,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range symlinks {
		hdr := &tar.Header{
			Name:     name,
			Typeflag: tar.TypeSymlink,
			Linkname: target,
			Mode:     0o777,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func buildTestZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestPerformSelfUpdateTarGzCreatesCuSymlinkAndRestartsService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix symlink test")
	}

	newBinary := []byte("#!/bin/sh\necho new-version\n")
	archive := buildTestTarGz(t, map[string][]byte{"check-usage": newBinary}, map[string]string{"cu": "check-usage"})
	archiveSum := sha256Hex(archive)
	assetName := "check-usage-linux-amd64.tar.gz"
	sumsContent := archiveSum + "  " + assetName + "\n"

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/McMelonTV/check-usage/releases/latest":
			if r.Header.Get("Accept") != "application/vnd.github+json" {
				t.Errorf("expected GitHub Accept header, got %q", r.Header.Get("Accept"))
			}
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v4",
				HTMLURL: "https://github.com/McMelonTV/check-usage/releases/tag/v4",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: server.URL + "/download/" + assetName, Digest: "sha256:" + archiveSum},
					{Name: cliChecksumsAssetName, BrowserDownloadURL: server.URL + "/download/" + cliChecksumsAssetName},
				},
			})
		case "/download/" + assetName:
			_, _ = w.Write(archive)
		case "/download/" + cliChecksumsAssetName:
			_, _ = w.Write([]byte(sumsContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "check-usage")
	if err := os.WriteFile(exePath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	restartedCalls := 0
	res, err := performSelfUpdate(context.Background(), updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
		CurrentVersion:    "v3",
		ExecutablePath:    exePath,
		HTTPClient:        server.Client(),
		RestartService: func(binaryPath string) (bool, error) {
			if binaryPath != exePath {
				t.Errorf("service synced to %q, want %q", binaryPath, exePath)
			}
			restartedCalls++
			return true, nil
		},
	})
	if err != nil {
		t.Fatalf("performSelfUpdate() error = %v", err)
	}
	if res.TargetVersion != "v4" || !res.AliasCreated || !res.ServiceRestarted || restartedCalls != 1 {
		t.Fatalf("unexpected update result: %#v (restartedCalls=%d)", res, restartedCalls)
	}

	gotBytes, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, newBinary) {
		t.Fatalf("updated binary = %q, want %q", gotBytes, newBinary)
	}

	cuPath := filepath.Join(dir, "cu")
	linkTarget, err := os.Readlink(cuPath)
	if err != nil {
		t.Fatalf("expected cu symlink at %s: %v", cuPath, err)
	}
	if linkTarget != "check-usage" {
		t.Fatalf("cu symlink target = %q, want %q", linkTarget, "check-usage")
	}
}

func TestPerformSelfUpdatePreservesUnrelatedCuBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix symlink test")
	}

	newBinary := []byte("new-binary")
	archive := buildTestTarGz(t, map[string][]byte{"check-usage": newBinary}, nil)
	assetName := "check-usage-linux-amd64.tar.gz"
	sumsContent := sha256Hex(archive) + "  " + assetName + "\n"

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/McMelonTV/check-usage/releases/latest":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v4",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: server.URL + "/download/" + assetName},
					{Name: cliChecksumsAssetName, BrowserDownloadURL: server.URL + "/download/" + cliChecksumsAssetName},
				},
			})
		case "/download/" + assetName:
			_, _ = w.Write(archive)
		case "/download/" + cliChecksumsAssetName:
			_, _ = w.Write([]byte(sumsContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "check-usage")
	cuPath := filepath.Join(dir, "cu")
	if err := os.WriteFile(exePath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cuPath, []byte("uucp-cu"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := performSelfUpdate(context.Background(), updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
		CurrentVersion:    "v3",
		ExecutablePath:    exePath,
		HTTPClient:        server.Client(),
		NoRestart:         true,
	})
	if err != nil {
		t.Fatalf("performSelfUpdate() error = %v", err)
	}
	if res.AliasCreated {
		t.Fatalf("expected AliasCreated=false when unrelated cu exists")
	}
	gotCu, err := os.ReadFile(cuPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCu) != "uucp-cu" {
		t.Fatalf("unrelated cu was clobbered: got %q", gotCu)
	}
}

func TestPerformSelfUpdateZipUpdatesSiblingWindowsAlias(t *testing.T) {
	oldBinary := []byte("old-win-bin")
	newBinary := []byte("new-win-bin")
	archive := buildTestZip(t, map[string][]byte{
		"check-usage.exe": newBinary,
		"cu.exe":          newBinary,
	})
	assetName := "check-usage-windows-amd64.zip"
	sumsContent := sha256Hex(archive) + "  " + assetName + "\n"

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/McMelonTV/check-usage/releases/tags/v4.1.0":
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v4.1.0",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: server.URL + "/download/" + assetName},
					{Name: cliChecksumsAssetName, BrowserDownloadURL: server.URL + "/download/" + cliChecksumsAssetName},
				},
			})
		case "/download/" + assetName:
			_, _ = w.Write(archive)
		case "/download/" + cliChecksumsAssetName:
			_, _ = w.Write([]byte(sumsContent))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "check-usage.exe")
	cuPath := filepath.Join(dir, "cu.exe")
	if err := os.WriteFile(exePath, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cuPath, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := performSelfUpdate(context.Background(), updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		TargetVersion:     "v4.1.0",
		GOOS:              "windows",
		GOARCH:            "amd64",
		CurrentVersion:    "v4.0.0",
		ExecutablePath:    exePath,
		HTTPClient:        server.Client(),
		NoRestart:         true,
	})
	if err != nil {
		t.Fatalf("performSelfUpdate() error = %v", err)
	}
	if !res.AliasUpdated {
		t.Fatalf("expected matching cu.exe to be updated, got %#v", res)
	}
	for _, p := range []string{exePath, cuPath} {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, newBinary) {
			t.Fatalf("%s = %q, want %q", p, got, newBinary)
		}
	}
}

func TestPerformSelfUpdateRejectsChecksumMismatchAndMissingChecksums(t *testing.T) {
	archive := buildTestTarGz(t, map[string][]byte{"check-usage": []byte("tampered")}, nil)
	assetName := "check-usage-linux-amd64.tar.gz"
	badSum := strings.Repeat("0", 64)

	t.Run("checksum mismatch", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/repos/McMelonTV/check-usage/releases/latest":
				_ = json.NewEncoder(w).Encode(githubRelease{
					TagName: "v4",
					Assets: []githubReleaseAsset{
						{Name: assetName, BrowserDownloadURL: server.URL + "/download/" + assetName},
						{Name: cliChecksumsAssetName, BrowserDownloadURL: server.URL + "/download/" + cliChecksumsAssetName},
					},
				})
			case "/download/" + assetName:
				_, _ = w.Write(archive)
			case "/download/" + cliChecksumsAssetName:
				_, _ = w.Write([]byte(badSum + "  " + assetName + "\n"))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		exePath := filepath.Join(t.TempDir(), "check-usage")
		if err := os.WriteFile(exePath, []byte("original"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := performSelfUpdate(context.Background(), updateOptions{
			APIBaseURL:        server.URL,
			AllowInsecureHTTP: true,
			GOOS:              "linux",
			GOARCH:            "amd64",
			CurrentVersion:    "v3",
			ExecutablePath:    exePath,
			HTTPClient:        server.Client(),
			NoRestart:         true,
		})
		if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("expected checksum mismatch error, got %v", err)
		}
		got, _ := os.ReadFile(exePath)
		if string(got) != "original" {
			t.Fatalf("original binary was modified on checksum failure: %q", got)
		}
	})

	t.Run("missing checksums", func(t *testing.T) {
		var server *httptest.Server
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(githubRelease{
				TagName: "v4",
				Assets: []githubReleaseAsset{
					{Name: assetName, BrowserDownloadURL: server.URL + "/download/" + assetName},
				},
			})
		}))
		defer server.Close()

		exePath := filepath.Join(t.TempDir(), "check-usage")
		if err := os.WriteFile(exePath, []byte("original"), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := performSelfUpdate(context.Background(), updateOptions{
			APIBaseURL:        server.URL,
			AllowInsecureHTTP: true,
			GOOS:              "linux",
			GOARCH:            "amd64",
			CurrentVersion:    "v3",
			ExecutablePath:    exePath,
			HTTPClient:        server.Client(),
			NoRestart:         true,
		})
		if err == nil || !strings.Contains(err.Error(), "verifiable SHA-256 checksum") {
			t.Fatalf("expected missing checksum error, got %v", err)
		}
	})
}

func TestPerformSelfUpdateCheckOnlyUpToDateAndMiseGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{
			TagName: "v4",
			HTMLURL: "https://github.com/McMelonTV/check-usage/releases/tag/v4",
		})
	}))
	defer server.Close()

	checkRes, err := performSelfUpdate(context.Background(), updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
		CurrentVersion:    "v3",
		CheckOnly:         true,
		HTTPClient:        server.Client(),
	})
	if err != nil {
		t.Fatalf("check-only error = %v", err)
	}
	if !checkRes.UpdateAvailable || checkRes.AlreadyUpToDate {
		t.Fatalf("unexpected check-only result: %#v", checkRes)
	}

	var syncedTo []string
	recordSync := func(binaryPath string) (bool, error) {
		syncedTo = append(syncedTo, binaryPath)
		return false, nil
	}
	upToDateRes, err := performSelfUpdate(context.Background(), updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
		CurrentVersion:    "4",
		ExecutablePath:    "/opt/check-usage",
		HTTPClient:        server.Client(),
		RestartService:    recordSync,
	})
	if err != nil {
		t.Fatalf("up-to-date error = %v", err)
	}
	if !upToDateRes.AlreadyUpToDate || len(syncedTo) != 1 || syncedTo[0] != "/opt/check-usage" {
		t.Fatalf("up-to-date run must still sync the service: %#v, synced %v", upToDateRes, syncedTo)
	}
}

// fakeMiseInstall lays out <data>/installs/<tool>/{3,4,latest->4} like mise does.
func fakeMiseInstall(t *testing.T) (toolDir, oldBinary, latestBinary string) {
	t.Helper()
	toolDir = filepath.Join(t.TempDir(), "mise", "installs", "github-mc-melon-tv-check-usage")
	for _, version := range []string{"3", "4"} {
		if err := os.MkdirAll(filepath.Join(toolDir, version), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(toolDir, version, "check-usage"), []byte("v"+version), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("./4", filepath.Join(toolDir, "latest")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, ".mise.backend.toml"), []byte("short = \"github:McMelonTV/check-usage\"\nfull = \"github:McMelonTV/check-usage\"\nexplicit_backend = true"), 0o644); err != nil {
		t.Fatal(err)
	}
	return toolDir, filepath.Join(toolDir, "3", "check-usage"), filepath.Join(toolDir, "latest", "check-usage")
}

func TestPerformSelfUpdateUpgradesMiseInstallsThroughMise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses symlinks")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v4"})
	}))
	defer server.Close()
	_, oldBinary, latestBinary := fakeMiseInstall(t)

	var upgraded []string
	var ignoredAge []bool
	var syncedTo []string
	opts := updateOptions{
		APIBaseURL:        server.URL,
		AllowInsecureHTTP: true,
		GOOS:              "linux",
		GOARCH:            "amd64",
		CurrentVersion:    "v3",
		ExecutablePath:    oldBinary,
		HTTPClient:        server.Client(),
		MiseUpgrade: func(tool string, ignoreAge bool) error {
			upgraded, ignoredAge = append(upgraded, tool), append(ignoredAge, ignoreAge)
			return nil
		},
		RestartService: func(binaryPath string) (bool, error) {
			syncedTo = append(syncedTo, binaryPath)
			return true, nil
		},
	}
	res, err := performSelfUpdate(context.Background(), opts)
	if err != nil {
		t.Fatalf("mise update error = %v", err)
	}
	if !res.ViaMise || res.HeldBack || res.ExecutablePath != latestBinary || !res.ServiceRestarted || ignoredAge[0] {
		t.Fatalf("unexpected mise update result: %#v", res)
	}
	if len(upgraded) != 1 || upgraded[0] != "github:McMelonTV/check-usage" {
		t.Fatalf("mise upgraded %v", upgraded)
	}
	// The service must follow the stable latest symlink, not a versioned path mise will prune.
	if len(syncedTo) != 1 || syncedTo[0] != latestBinary {
		t.Fatalf("service synced to %v, want %s", syncedTo, latestBinary)
	}

	// Running from the newest install while GitHub reports a newer tag: mise
	// held the release back, so cu update must not claim it upgraded.
	opts.ExecutablePath = filepath.Join(filepath.Dir(filepath.Dir(oldBinary)), "4", "check-usage")
	opts.TargetVersion, opts.IgnoreAge = "", true
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(githubRelease{TagName: "v5"})
	})
	opts.CurrentVersion = "v4"
	res, err = performSelfUpdate(context.Background(), opts)
	if err != nil || !res.HeldBack || !ignoredAge[1] {
		t.Fatalf("held-back mise update misreported: %#v, %v (ignoredAge=%v)", res, err, ignoredAge)
	}

	opts.TargetVersion = "v4"
	if _, err := performSelfUpdate(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "mise use github:McMelonTV/check-usage@4") {
		t.Fatalf("pinned mise update should point at mise use, got %v", err)
	}
	if len(upgraded) != 2 {
		t.Fatal("pinned mise update must not run mise upgrade")
	}
}

func TestMiseUpgradeWritesLatestAndRespectsReleaseAgeByDefault(t *testing.T) {
	if got := strings.Join(miseUpgradeArgs("github:McMelonTV/check-usage", false), " "); got != "upgrade github:McMelonTV/check-usage@latest --bump" {
		t.Fatalf("default mise args = %q", got)
	}
	if got := strings.Join(miseUpgradeArgs("github:McMelonTV/check-usage", true), " "); got != "upgrade github:McMelonTV/check-usage@latest --bump --minimum-release-age 0" {
		t.Fatalf("--ignore-age mise args = %q", got)
	}
}

func TestStableServiceBinaryPathFollowsMiseLatest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses symlinks")
	}
	toolDir, oldBinary, latestBinary := fakeMiseInstall(t)
	newBinary := filepath.Join(toolDir, "4", "check-usage")
	if got := stableServiceBinaryPath(newBinary); got != latestBinary {
		t.Fatalf("newest install should use %s, got %s", latestBinary, got)
	}
	// A pinned older version keeps its own path instead of jumping to latest.
	if got := stableServiceBinaryPath(oldBinary); got != oldBinary {
		t.Fatalf("pinned install should keep %s, got %s", oldBinary, got)
	}
	if got := stableServiceBinaryPath("/usr/local/bin/check-usage"); got != "/usr/local/bin/check-usage" {
		t.Fatalf("non-mise path changed to %s", got)
	}
	if isMiseManagedPath(filepath.Join(toolDir, "4")) || !isMiseManagedPath(newBinary) {
		t.Fatal("mise install detection is wrong")
	}
}

func TestWatchServiceBinaryNoticesUpgrades(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses symlinks")
	}
	toolDir, _, latestBinary := fakeMiseInstall(t)
	changed := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchServiceBinary(ctx, latestBinary, 10*time.Millisecond, func() { close(changed) })

	select {
	case <-changed:
		t.Fatal("restarted without an upgrade")
	case <-time.After(50 * time.Millisecond):
	}
	// mise installs version 5 and repoints latest.
	if err := os.MkdirAll(filepath.Join(toolDir, "5"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(toolDir, "5", "check-usage"), []byte("v5"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(toolDir, "latest")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("./5", filepath.Join(toolDir, "latest")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("upgrade was not noticed")
	}
}

func TestVersionCommandsAndComparison(t *testing.T) {
	if !sameVersion("v4", "4") || !sameVersion("v0.4.0", "0.4.0") {
		t.Fatalf("expected version normalization to match")
	}
	if sameVersion("dev", "dev") || sameVersion("v3", "v4") {
		t.Fatalf("unexpected sameVersion match")
	}
	if code := Run([]string{"cu", "version"}); code != 0 {
		t.Fatalf("cu version exited with %d", code)
	}
	if code := Run([]string{"cu", "--version"}); code != 0 {
		t.Fatalf("cu --version exited with %d", code)
	}
}
