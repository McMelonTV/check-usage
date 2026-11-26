package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	defaultUpdateRepo       = "McMelonTV/check-usage"
	defaultGitHubAPIBaseURL = "https://api.github.com"
	cliChecksumsAssetName   = "check-usage-SHA256SUMS.txt"
	maxUpdateMetadataBytes  = 4 << 20   // 4 MiB
	maxUpdateAssetBytes     = 100 << 20 // 100 MiB
	maxUpdateBinaryBytes    = 100 << 20 // 100 MiB
)

type githubRelease struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

type updateOptions struct {
	Repo              string
	APIBaseURL        string
	TargetVersion     string
	CheckOnly         bool
	Force             bool
	IgnoreAge         bool
	NoRestart         bool
	GOOS              string
	GOARCH            string
	CurrentVersion    string
	ExecutablePath    string
	HTTPClient        *http.Client
	AuthToken         string
	AllowInsecureHTTP bool
	// RestartService brings an installed service onto binaryPath: it rewrites
	// an outdated unit and restarts the service when it runs an older binary.
	RestartService func(binaryPath string) (bool, error)
	// MiseUpgrade upgrades a mise-managed install to the newest release.
	MiseUpgrade func(tool string, ignoreAge bool) error
}

type updateResult struct {
	CurrentVersion   string
	TargetVersion    string
	ReleaseURL       string
	AssetName        string
	ExecutablePath   string
	AlreadyUpToDate  bool
	UpdateAvailable  bool
	AliasCreated     bool
	AliasUpdated     bool
	ViaMise          bool
	HeldBack         bool // mise kept the current version (minimum release age)
	ServiceRestarted bool
	RestartWarning   error
}

func runUpdateCommand(args []string) int {
	fs := flag.NewFlagSet("check-usage update", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	setDoubleDashFlagUsage(fs)

	checkOnly := fs.Bool("check", false, "check if a newer release is available without installing")
	targetVersion := fs.String("version", "", "install a specific release tag (default: latest)")
	force := fs.Bool("force", false, "reinstall even if already on the target version, and replace a mise-managed binary in place instead of running mise")
	ignoreAge := fs.Bool("ignore-age", false, "for mise installs, install the newest release even if it is newer than mise's minimum release age")
	noRestart := fs.Bool("no-restart", false, "do not restart an active systemd user service after updating")
	timeout := fs.Int("timeout", 120, "download timeout in seconds")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fs.Usage()
		return 2
	}

	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token == "" {
		token = strings.TrimSpace(os.Getenv("GH_TOKEN"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeout)*time.Second)
	defer cancel()

	res, err := performSelfUpdate(ctx, updateOptions{
		TargetVersion: strings.TrimSpace(*targetVersion),
		CheckOnly:     *checkOnly,
		Force:         *force,
		IgnoreAge:     *ignoreAge,
		NoRestart:     *noRestart,
		HTTPClient:    &http.Client{Timeout: time.Duration(*timeout) * time.Second},
		AuthToken:     token,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if *checkOnly {
		if res.AlreadyUpToDate {
			fmt.Printf("Up to date (%s).\n", res.TargetVersion)
			return 0
		}
		if res.ReleaseURL != "" {
			fmt.Printf("Update available: %s -> %s (%s)\n", res.CurrentVersion, res.TargetVersion, res.ReleaseURL)
		} else {
			fmt.Printf("Update available: %s -> %s\n", res.CurrentVersion, res.TargetVersion)
		}
		return 0
	}

	switch {
	case res.AlreadyUpToDate:
		fmt.Printf("Already up to date (%s).\n", res.TargetVersion)
	case res.HeldBack:
		fmt.Printf("mise kept check-usage at %s: %s is newer than mise's minimum release age.\nRerun with --ignore-age to install it now.\n", res.CurrentVersion, res.TargetVersion)
	case res.ViaMise:
		fmt.Printf("Updated check-usage from %s to %s with mise.\n", res.CurrentVersion, res.TargetVersion)
	default:
		fmt.Printf("Updated check-usage from %s to %s (%s).\n", res.CurrentVersion, res.TargetVersion, res.ExecutablePath)
	}
	if res.AliasCreated {
		if runtime.GOOS == "windows" {
			fmt.Println("Installed cu.exe alias alongside check-usage.exe.")
		} else {
			fmt.Println("Created cu symlink -> check-usage.")
		}
	} else if res.AliasUpdated {
		fmt.Println("Updated sibling cu alias.")
	}
	if res.RestartWarning != nil {
		fmt.Fprintf(os.Stderr, "warning: binary updated, but restarting %s failed: %v\n", "check-usage.service", res.RestartWarning)
	} else if res.ServiceRestarted {
		fmt.Println("Restarted check-usage.service.")
	}
	return 0
}

func performSelfUpdate(ctx context.Context, opts updateOptions) (updateResult, error) {
	if opts.Repo == "" {
		opts.Repo = defaultUpdateRepo
	}
	if opts.APIBaseURL == "" {
		opts.APIBaseURL = defaultGitHubAPIBaseURL
	}
	if opts.GOOS == "" {
		opts.GOOS = runtime.GOOS
	}
	if opts.GOARCH == "" {
		opts.GOARCH = runtime.GOARCH
	}
	if opts.CurrentVersion == "" {
		opts.CurrentVersion = CurrentVersion()
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	}
	if opts.RestartService == nil {
		opts.RestartService = syncSystemdService
	}
	if opts.MiseUpgrade == nil {
		opts.MiseUpgrade = runMiseUpgrade
	}

	assetName, err := releaseAssetName(opts.GOOS, opts.GOARCH)
	if err != nil {
		return updateResult{}, err
	}

	release, err := fetchGitHubRelease(ctx, opts)
	if err != nil {
		return updateResult{}, err
	}
	if strings.TrimSpace(release.TagName) == "" {
		return updateResult{}, errors.New("release response did not include a tag name")
	}

	res := updateResult{
		CurrentVersion: opts.CurrentVersion,
		TargetVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
		AssetName:      assetName,
	}

	upToDate := sameVersion(opts.CurrentVersion, release.TagName)
	if opts.CheckOnly {
		res.AlreadyUpToDate = upToDate
		res.UpdateAvailable = !upToDate
		return res, nil
	}

	exePath := opts.ExecutablePath
	if exePath == "" {
		exePath, err = resolveSelfExecutablePath()
		if err != nil {
			return res, fmt.Errorf("resolve current executable path: %w", err)
		}
	}
	res.ExecutablePath = exePath

	if upToDate && !opts.Force {
		res.AlreadyUpToDate = true
		// The binary may already be new (for example after `mise upgrade`)
		// while the service still runs an older one or an outdated unit.
		restartService(&res, opts, stableServiceBinaryPath(exePath))
		return res, nil
	}

	if !opts.Force && isMiseManagedPath(exePath) {
		tool := miseToolName(exePath, "github:"+opts.Repo)
		if opts.TargetVersion != "" {
			return res, fmt.Errorf("this binary is managed by mise (%s); run `mise use %s@%s` to install a specific release, or rerun with --force to replace it in place", exePath, tool, strings.TrimPrefix(opts.TargetVersion, "v"))
		}
		if err := opts.MiseUpgrade(tool, opts.IgnoreAge); err != nil {
			return res, err
		}
		res.ViaMise = true
		install, _ := parseMiseInstall(exePath)
		latest := filepath.Join(install.ToolDir, "latest", install.Rel)
		if after, err := os.Stat(latest); err == nil {
			// mise exits successfully when its release age holds the update back;
			// latest still pointing at this binary means nothing was installed.
			if current, err := os.Stat(exePath); err == nil && os.SameFile(current, after) {
				res.HeldBack = true
			}
			res.ExecutablePath = latest
		}
		restartService(&res, opts, res.ExecutablePath)
		return res, nil
	}

	archiveAsset, ok := findReleaseAsset(release.Assets, assetName)
	if !ok {
		return res, fmt.Errorf("release %s does not include %s", release.TagName, assetName)
	}

	expectedSHA, err := resolveExpectedAssetSHA256(ctx, opts, release, archiveAsset)
	if err != nil {
		return res, err
	}

	archiveBytes, err := downloadBytes(ctx, opts.HTTPClient, archiveAsset.BrowserDownloadURL, false, "", opts.AllowInsecureHTTP, maxUpdateAssetBytes)
	if err != nil {
		return res, fmt.Errorf("download %s: %w", archiveAsset.Name, err)
	}

	actualSum := sha256.Sum256(archiveBytes)
	actualHex := hex.EncodeToString(actualSum[:])
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(expectedSHA)), []byte(actualHex)) != 1 {
		return res, fmt.Errorf("SHA-256 checksum mismatch for %s (got %s, expected %s)", archiveAsset.Name, actualHex, expectedSHA)
	}

	binaryName := "check-usage"
	if opts.GOOS == "windows" {
		binaryName = "check-usage.exe"
	}

	var binaryBytes []byte
	switch {
	case strings.HasSuffix(archiveAsset.Name, ".tar.gz"):
		binaryBytes, err = extractBinaryFromTarGz(archiveBytes, binaryName)
	case strings.HasSuffix(archiveAsset.Name, ".zip"):
		binaryBytes, err = extractBinaryFromZip(archiveBytes, binaryName)
	default:
		err = fmt.Errorf("unsupported release archive format: %s", archiveAsset.Name)
	}
	if err != nil {
		return res, fmt.Errorf("extract %s from %s: %w", binaryName, archiveAsset.Name, err)
	}

	prevDigest, _ := fileSHA256(exePath)
	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(exePath); statErr == nil {
		mode = info.Mode().Perm()
		if opts.GOOS != "windows" {
			mode |= 0o111
		}
	}

	if err := replaceBinaryFile(exePath, binaryBytes, mode, opts.GOOS == "windows"); err != nil {
		return res, fmt.Errorf("replace executable %s: %w", exePath, err)
	}

	created, updated, aliasErr := ensureCuAlias(exePath, prevDigest, binaryBytes, mode, opts.GOOS)
	if aliasErr == nil {
		res.AliasCreated = created
		res.AliasUpdated = updated
	}

	restartService(&res, opts, exePath)
	return res, nil
}

func restartService(res *updateResult, opts updateOptions, binaryPath string) {
	if opts.NoRestart || opts.RestartService == nil {
		return
	}
	restarted, err := opts.RestartService(binaryPath)
	res.ServiceRestarted = restarted && err == nil
	res.RestartWarning = err
}

func releaseAssetName(goos, goarch string) (string, error) {
	var osPart string
	switch goos {
	case "linux":
		osPart = "linux"
	case "darwin":
		osPart = "macos"
	case "windows":
		osPart = "windows"
	default:
		return "", fmt.Errorf("unsupported platform for self-update: %s/%s", goos, goarch)
	}

	switch goarch {
	case "amd64", "arm64":
	default:
		return "", fmt.Errorf("unsupported platform for self-update: %s/%s", goos, goarch)
	}

	if goos == "windows" {
		return fmt.Sprintf("check-usage-%s-%s.zip", osPart, goarch), nil
	}
	return fmt.Sprintf("check-usage-%s-%s.tar.gz", osPart, goarch), nil
}

func fetchGitHubRelease(ctx context.Context, opts updateOptions) (githubRelease, error) {
	base := strings.TrimRight(opts.APIBaseURL, "/")
	var endpoint string
	if opts.TargetVersion != "" {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/tags/%s", base, opts.Repo, url.PathEscape(opts.TargetVersion))
	} else {
		endpoint = fmt.Sprintf("%s/repos/%s/releases/latest", base, opts.Repo)
	}

	body, err := downloadBytes(ctx, opts.HTTPClient, endpoint, true, opts.AuthToken, opts.AllowInsecureHTTP, maxUpdateMetadataBytes)
	if err != nil {
		return githubRelease{}, fmt.Errorf("fetch release metadata: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return githubRelease{}, fmt.Errorf("decode release metadata: %w", err)
	}
	return release, nil
}

func findReleaseAsset(assets []githubReleaseAsset, name string) (githubReleaseAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubReleaseAsset{}, false
}

func resolveExpectedAssetSHA256(ctx context.Context, opts updateOptions, release githubRelease, archiveAsset githubReleaseAsset) (string, error) {
	var apiDigest string
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(archiveAsset.Digest)), "sha256:") {
		candidate := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(archiveAsset.Digest), "sha256:"))
		if isValidSHA256Hex(candidate) {
			apiDigest = candidate
		}
	}

	var fileDigest string
	if sumsAsset, ok := findReleaseAsset(release.Assets, cliChecksumsAssetName); ok {
		sumsBytes, err := downloadBytes(ctx, opts.HTTPClient, sumsAsset.BrowserDownloadURL, false, "", opts.AllowInsecureHTTP, maxUpdateMetadataBytes)
		if err != nil {
			return "", fmt.Errorf("download %s: %w", cliChecksumsAssetName, err)
		}
		parsed, ok := parseSHA256Sums(sumsBytes, archiveAsset.Name)
		if !ok {
			return "", fmt.Errorf("%s in release %s does not list %s", cliChecksumsAssetName, release.TagName, archiveAsset.Name)
		}
		fileDigest = parsed
	}

	if fileDigest != "" && apiDigest != "" && !strings.EqualFold(fileDigest, apiDigest) {
		return "", fmt.Errorf("conflicting SHA-256 checksums for %s between %s and GitHub release metadata", archiveAsset.Name, cliChecksumsAssetName)
	}
	if fileDigest != "" {
		return fileDigest, nil
	}
	if apiDigest != "" {
		return apiDigest, nil
	}
	return "", fmt.Errorf("release %s does not provide a verifiable SHA-256 checksum (%s) for %s", release.TagName, cliChecksumsAssetName, archiveAsset.Name)
}

func parseSHA256Sums(data []byte, assetName string) (string, bool) {
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		sum := strings.ToLower(strings.TrimSpace(fields[0]))
		name := strings.TrimPrefix(strings.TrimSpace(fields[1]), "*")
		name = strings.TrimPrefix(name, "./")
		if name == assetName && isValidSHA256Hex(sum) {
			return sum, true
		}
	}
	return "", false
}

func isValidSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func downloadBytes(ctx context.Context, client *http.Client, rawURL string, githubAPI bool, authToken string, allowInsecureHTTP bool, limit int64) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != "https" && !(allowInsecureHTTP && parsed.Scheme == "http") {
		return nil, fmt.Errorf("refusing non-HTTPS download URL: %s", rawURL)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "check-usage/"+CurrentVersion())
	if githubAPI {
		req.Header.Set("Accept", "application/vnd.github+json")
		if authToken != "" {
			req.Header.Set("Authorization", "Bearer "+authToken)
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		msg := strings.TrimSpace(string(snippet))
		if msg != "" {
			return nil, fmt.Errorf("HTTP %d from %s: %s", resp.StatusCode, parsed.Host, msg)
		}
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, parsed.Host)
	}

	limited := io.LimitReader(resp.Body, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response from %s exceeded size limit (%d bytes)", parsed.Host, limit)
	}
	return data, nil
}

func extractBinaryFromTarGz(archive []byte, binaryName string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if !isSafeArchiveEntry(hdr.Name) {
			continue
		}
		if filepath.Base(hdr.Name) != binaryName {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, maxUpdateBinaryBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxUpdateBinaryBytes {
			return nil, fmt.Errorf("extracted binary %s exceeded size limit", binaryName)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("extracted binary %s is empty", binaryName)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func extractBinaryFromZip(archive []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !isSafeArchiveEntry(f.Name) {
			continue
		}
		if !strings.EqualFold(filepath.Base(f.Name), binaryName) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxUpdateBinaryBytes+1))
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxUpdateBinaryBytes {
			return nil, fmt.Errorf("extracted binary %s exceeded size limit", binaryName)
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("extracted binary %s is empty", binaryName)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%s not found in archive", binaryName)
}

func isSafeArchiveEntry(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) {
		return false
	}
	normalized := strings.ReplaceAll(name, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}

func resolveSelfExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return filepath.Abs(exe)
}

func fileSHA256(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func replaceBinaryFile(targetPath string, data []byte, mode os.FileMode, windowsMode bool) error {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, ".check-usage-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if mode == 0 {
		mode = 0o755
	}
	if !windowsMode {
		if err := tmp.Chmod(mode); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if !windowsMode {
		if err := os.Rename(tmpPath, targetPath); err != nil {
			return err
		}
		cleanup = false
		return nil
	}

	backupPath := targetPath + ".old"
	_ = os.Remove(backupPath)
	hadExisting := false
	if _, statErr := os.Stat(targetPath); statErr == nil {
		if err := os.Rename(targetPath, backupPath); err != nil {
			return err
		}
		hadExisting = true
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, targetPath)
		}
		return err
	}
	cleanup = false
	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
}

func ensureCuAlias(exePath string, prevDigest, newData []byte, mode os.FileMode, goos string) (created bool, updated bool, err error) {
	dir := filepath.Dir(exePath)
	base := filepath.Base(exePath)

	if goos != "windows" {
		if base != "check-usage" {
			return false, false, nil
		}
		cuPath := filepath.Join(dir, "cu")
		if _, err := os.Lstat(cuPath); err == nil {
			// Leave existing cu untouched (either already our symlink or an unrelated command).
			return false, false, nil
		} else if !os.IsNotExist(err) {
			return false, false, err
		}
		if err := os.Symlink("check-usage", cuPath); err != nil {
			return false, false, err
		}
		return true, false, nil
	}

	var siblingName string
	allowCreate := false
	switch {
	case strings.EqualFold(base, "check-usage.exe"):
		siblingName = "cu.exe"
		allowCreate = true
	case strings.EqualFold(base, "cu.exe"):
		siblingName = "check-usage.exe"
		allowCreate = false
	default:
		return false, false, nil
	}

	siblingPath := filepath.Join(dir, siblingName)
	_, statErr := os.Stat(siblingPath)
	if os.IsNotExist(statErr) {
		if !allowCreate {
			return false, false, nil
		}
		if err := replaceBinaryFile(siblingPath, newData, mode, true); err != nil {
			return false, false, err
		}
		return true, false, nil
	}
	if statErr != nil {
		return false, false, statErr
	}
	if len(prevDigest) == 0 {
		return false, false, nil
	}
	siblingDigest, err := fileSHA256(siblingPath)
	if err != nil {
		return false, false, err
	}
	if !bytes.Equal(siblingDigest, prevDigest) {
		return false, false, nil
	}
	if err := replaceBinaryFile(siblingPath, newData, mode, true); err != nil {
		return false, false, err
	}
	return false, true, nil
}
