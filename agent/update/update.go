package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/dengyie/mangohub/agent/dnsresolver"
	"github.com/inconshreveable/go-update"
	"github.com/rhysd/go-github-selfupdate/selfupdate"
)

var ErrRestartRequired = errors.New("update installed; restart required")

var (
	CurrentVersion string = "Snapshot-260911-mining"
	Repo           string = "dengyie/mangohub"
)

const (
	snapshotVersionPrefix = "Snapshot-"
	containerMarkerPath   = "/.mango-agent-container"
	githubAPIBaseURL      = "https://api.github.com"
)

type buildTrack int

const (
	stableTrack buildTrack = iota
	snapshotTrack
)

type githubRelease struct {
	TagName     string               `json:"tag_name"`
	Name        string               `json:"name"`
	Body        string               `json:"body"`
	Draft       bool                 `json:"draft"`
	Prerelease  bool                 `json:"prerelease"`
	HTMLURL     string               `json:"html_url"`
	PublishedAt time.Time            `json:"published_at"`
	Assets      []githubReleaseAsset `json:"assets"`
}

type githubReleaseAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	Size               int    `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type snapshotReleaseCandidate struct {
	TagName     string
	Name        string
	Body        string
	HTMLURL     string
	PublishedAt time.Time
	Asset       githubReleaseAsset
}

type selfUpdater interface {
	UpdateSelf(current semver.Version, slug string) (*selfupdate.Release, error)
	UpdateTo(release *selfupdate.Release, cmdPath string) error
}

type releaseLister func(owner, repo string) ([]githubRelease, error)
type containerCheck func() bool

// parseVersion 解析可能带有 v/V 前缀，以及预发布或构建元数据的版本字符串
func parseVersion(ver string) (semver.Version, error) {
	ver = strings.TrimPrefix(ver, "v")
	ver = strings.TrimPrefix(ver, "V")
	return semver.ParseTolerant(ver)
}

// needUpdate 判断是否需要更新
func needUpdate(current, latest semver.Version) bool {
	// 返回最新版本大于当前版本时需要更新
	return latest.Compare(current) > 0
}

func detectBuildTrack(version string) buildTrack {
	if strings.HasPrefix(version, snapshotVersionPrefix) {
		return snapshotTrack
	}
	return stableTrack
}

func expectedAssetName(goos, goarch string) string {
	name := fmt.Sprintf("mangohub-agent-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func legacyAssetName(goos, goarch string) string {
	name := fmt.Sprintf("komari-agent-%s-%s", goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func findReleaseAsset(release githubRelease, assetName string) (githubReleaseAsset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			return asset, true
		}
	}
	// 回退检查旧版命名的二进制产物
	legacyName := strings.Replace(assetName, "mangohub-agent-", "komari-agent-", 1)
	if legacyName != assetName {
		for _, asset := range release.Assets {
			if asset.Name == legacyName {
				return asset, true
			}
		}
	}
	return githubReleaseAsset{}, false
}

func selectLatestSnapshotRelease(releases []githubRelease, assetName string) (snapshotReleaseCandidate, bool) {
	var latest snapshotReleaseCandidate
	found := false

	for _, release := range releases {
		if release.Draft || !release.Prerelease || !strings.HasPrefix(release.TagName, snapshotVersionPrefix) {
			continue
		}

		asset, ok := findReleaseAsset(release, assetName)
		if !ok {
			continue
		}

		candidate := snapshotReleaseCandidate{
			TagName:     release.TagName,
			Name:        release.Name,
			Body:        release.Body,
			HTMLURL:     release.HTMLURL,
			PublishedAt: release.PublishedAt,
			Asset:       asset,
		}

		if !found ||
			candidate.PublishedAt.After(latest.PublishedAt) ||
			(candidate.PublishedAt.Equal(latest.PublishedAt) && candidate.TagName > latest.TagName) {
			latest = candidate
			found = true
		}
	}

	return latest, found
}

// snapshotNeedsUpdate compares the snapshot tag against the running version.
// A plain inequality downgraded the fleet: re-publishing an OLDER prerelease
// tag made every agent "update" backwards. Snapshot tags are date-encoded
// (Snapshot-YYMMDD-<name>, e.g. Snapshot-260911-mining), so the numeric core
// orders them; equal cores never update. Unknown schemes fall back to the
// legacy inequality.
func snapshotNeedsUpdate(currentVersion string, latest snapshotReleaseCandidate) bool {
	if currentVersion == latest.TagName {
		return false
	}
	currentCore := snapshotVersionCore(currentVersion)
	latestCore := snapshotVersionCore(latest.TagName)
	if currentCore != "" && latestCore != "" {
		if currentCore == latestCore {
			return false
		}
		return latestCore > currentCore
	}
	return true
}

// snapshotVersionCore extracts the leading numeric date from a snapshot tag
// (Snapshot-260911-mining -> 260911). Empty when the scheme differs.
func snapshotVersionCore(tag string) string {
	trimmed := strings.TrimPrefix(tag, snapshotVersionPrefix)
	digits := trimmed
	for i, r := range trimmed {
		if r < '0' || r > '9' {
			digits = trimmed[:i]
			break
		}
	}
	if digits == "" || len(digits) != len(trimmed) && digits == trimmed[:0] {
		return ""
	}
	if len(digits) < 4 {
		return ""
	}
	return digits
}

func isContainerAgent() bool {
	_, err := os.Stat(containerMarkerPath)
	return err == nil
}

func splitRepoSlug(slug string) (string, string, error) {
	parts := strings.Split(slug, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo slug %q, expected owner/name", slug)
	}
	return parts[0], parts[1], nil
}

func listGitHubReleases(owner, repo string) ([]githubRelease, error) {
	var releases []githubRelease

	for page := 1; ; page++ {
		endpoint := fmt.Sprintf(
			"%s/repos/%s/%s/releases?per_page=100&page=%d",
			githubAPIBaseURL,
			url.PathEscape(owner),
			url.PathEscape(repo),
			page,
		)
		req, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create GitHub releases request: %w", err)
		}

		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "mangohub-agent")
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to list GitHub releases: %w", err)
		}

		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			return nil, fmt.Errorf("GitHub releases API returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}

		var pageReleases []githubRelease
		if err := json.NewDecoder(resp.Body).Decode(&pageReleases); err != nil {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("failed to decode GitHub releases response: %w", err)
		}
		_ = resp.Body.Close()

		releases = append(releases, pageReleases...)
		if len(pageReleases) < 100 {
			return releases, nil
		}
	}
}

func currentExecutablePath() (string, error) {
	cmdPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(cmdPath, ".exe") {
		cmdPath += ".exe"
	}

	stat, err := os.Lstat(cmdPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat %q: %w", cmdPath, err)
	}
	if stat.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(cmdPath)
		if err != nil {
			return "", fmt.Errorf("failed to resolve symlink %q for executable: %w", cmdPath, err)
		}
		cmdPath = resolved
	}

	return cmdPath, nil
}

// snapshotChecksumEntry fetches checksums.txt from the same snapshot release
// and returns the expected hex SHA-256 for assetName. Empty string when the
// release carries no checksums.txt (pre-dating the release workflow) — the
// caller then skips verification, matching install.sh's tolerant behaviour.
func snapshotChecksumEntry(ctx context.Context, client *http.Client, releases []githubRelease, tagName, assetName string) string {
	for _, release := range releases {
		if release.TagName != tagName {
			continue
		}
		for _, asset := range release.Assets {
			if asset.Name != "checksums.txt" {
				continue
			}
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
			if err != nil {
				return ""
			}
			resp, err := client.Do(req)
			if err != nil {
				return ""
			}
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(strings.TrimSpace(line))
				if len(fields) == 2 && strings.EqualFold(fields[1], assetName) && len(fields[0]) == 64 {
					return strings.ToLower(fields[0])
				}
			}
		}
		break
	}
	return ""
}

// downloadVerifiedSnapshot fetches the snapshot asset, verifies its SHA-256
// against the release checksums.txt (when present), and returns the payload.
// A mismatch aborts the update: the binary runs as root on the monitored
// machine, so a corrupted or tampered download must never replace it.
func downloadVerifiedSnapshot(ctx context.Context, client *http.Client, candidate snapshotReleaseCandidate, expectedSHA256 string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, candidate.Asset.BrowserDownloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create download request: %w", err)
	}
	req.Header.Add("Accept", "application/octet-stream")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download snapshot asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("snapshot asset download returned status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshotAssetBytes))
	if err != nil {
		return nil, fmt.Errorf("failed reading snapshot asset body: %w", err)
	}
	if expectedSHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), expectedSHA256) {
			return nil, fmt.Errorf("snapshot asset checksum mismatch: expected %s, got %x — update aborted", expectedSHA256, sum)
		}
		log.Printf("Snapshot asset checksum verified (%s)", expectedSHA256)
	}
	return data, nil
}

// maxSnapshotAssetBytes rejects absurd downloads (agent binaries are ~10-20MB;
// 256MiB leaves generous headroom for debug builds).
const maxSnapshotAssetBytes = 256 << 20

func DoUpdateWorks(onRestartRequired func()) {
	ticker_ := time.NewTicker(time.Duration(6) * time.Hour)
	defer ticker_.Stop()
	runScheduledUpdates(ticker_.C, CheckAndUpdate, onRestartRequired)
}

func runScheduledUpdates(ticks <-chan time.Time, check func() error, onRestartRequired func()) {
	for range ticks {
		if errors.Is(check(), ErrRestartRequired) {
			if onRestartRequired != nil {
				onRestartRequired()
			}
			return
		}
	}
}

func checkAndUpdateStable(currentSemVer semver.Version, updater selfUpdater) error {
	latest, err := updater.UpdateSelf(currentSemVer, Repo)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %v", err)
	}

	if latest.Version.Equals(currentSemVer) {
		log.Println("Current version is the latest:", CurrentVersion)
		return nil
	}
	// Default is installed as a service, so don't automatically restart
	//execPath, err := os.Executable()
	//if err != nil {
	//	return fmt.Errorf("failed to get current executable path: %v", err)
	//}

	// _, err = os.StartProcess(execPath, os.Args, &os.ProcAttr{
	// 	Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	// })
	// if err != nil {
	// 	return fmt.Errorf("failed to restart program: %v", err)
	// }
	log.Printf("Successfully updated to version %s\n", latest.Version)
	return ErrRestartRequired
}

func checkAndUpdateSnapshot(updater selfUpdater, listReleases releaseLister, isContainer containerCheck) error {
	if isContainer() {
		log.Println("Snapshot agent is running in a container; skip binary self-update. Refresh the ghcr.io image tagged 'snapshot' instead.")
		return nil
	}

	owner, repo, err := splitRepoSlug(Repo)
	if err != nil {
		return err
	}

	releases, err := listReleases(owner, repo)
	if err != nil {
		return err
	}

	assetName := expectedAssetName(runtime.GOOS, runtime.GOARCH)
	latest, found := selectLatestSnapshotRelease(releases, assetName)
	if !found {
		log.Printf("No suitable snapshot release asset was found for %s. Current snapshot is considered up-to-date.", assetName)
		return nil
	}

	if !snapshotNeedsUpdate(CurrentVersion, latest) {
		log.Println("Current snapshot version is the latest:", CurrentVersion)
		return nil
	}

	cmdPath, err := currentExecutablePath()
	if err != nil {
		return fmt.Errorf("failed to resolve current executable path: %w", err)
	}

	log.Printf("Will update %s from snapshot %s to %s\n", cmdPath, CurrentVersion, latest.TagName)

	// Integrity gate: fetch checksums.txt from the release, download the asset,
	// verify SHA-256, and only then swap the binary (go-update applies atomically
	// with rollback). Older snapshots without checksums.txt fall back to a plain
	// download — same tolerance install.sh shows.
	client := dnsresolver.GetHTTPClient(5 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	expected := snapshotChecksumEntry(ctx, client, releases, latest.TagName, assetName)
	data, err := downloadVerifiedSnapshot(ctx, client, latest, expected)
	if err != nil {
		return fmt.Errorf("failed to fetch snapshot %s: %w", latest.TagName, err)
	}

	if err := applySnapshotUpdate(bytes.NewReader(data), latest.Asset.BrowserDownloadURL, cmdPath); err != nil {
		return fmt.Errorf("failed to update to snapshot %s: %w", latest.TagName, err)
	}

	log.Printf("Successfully updated to snapshot version %s\n", latest.TagName)
	return ErrRestartRequired
}

// applySnapshotUpdate mirrors selfupdate's uncompressAndUpdate: unpack the
// asset (gz/xz/tar-or-raw depending on the asset name) and swap the running
// binary via go-update. It exists so the integrity check can sit between
// download and apply — the library's UpdateTo offers no pre-apply hook.
func applySnapshotUpdate(src io.Reader, assetURL, cmdPath string) error {
	_, cmd := filepath.Split(cmdPath)
	asset, err := selfupdate.UncompressCommand(src, assetURL, cmd)
	if err != nil {
		return err
	}
	return update.Apply(asset, update.Options{TargetPath: cmdPath})
}

// 检查更新并执行自动更新
func CheckAndUpdate() error {
	log.Println("Checking update...")

	http.DefaultClient = dnsresolver.GetHTTPClient(60 * time.Second)
	updater, err := selfupdate.NewUpdater(selfupdate.Config{})
	if err != nil {
		return fmt.Errorf("failed to create updater: %v", err)
	}

	if detectBuildTrack(CurrentVersion) == snapshotTrack {
		return checkAndUpdateSnapshot(updater, listGitHubReleases, isContainerAgent)
	}

	currentSemVer, err := parseVersion(CurrentVersion)
	if err != nil {
		return fmt.Errorf("failed to parse current version: %v", err)
	}

	return checkAndUpdateStable(currentSemVer, updater)
}
