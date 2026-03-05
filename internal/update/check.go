package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ReleaseInfo mirrors the relevant fields from the GitHub releases API.
type ReleaseInfo struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset mirrors a single release asset from the GitHub API.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

const releaseURL = "https://api.github.com/repos/Azmekk/localsync/releases/latest"

// CheckLatest queries GitHub for the latest release.
func CheckLatest() (*ReleaseInfo, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(releaseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var info ReleaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// StartBackgroundCheck runs CheckLatest in a goroutine and returns a channel
// that will receive the result if an update is available.
func StartBackgroundCheck() <-chan *ReleaseInfo {
	ch := make(chan *ReleaseInfo, 1)
	if Version == "dev" {
		close(ch)
		return ch
	}
	go func() {
		defer close(ch)
		info, err := CheckLatest()
		if err != nil {
			return
		}
		if CompareVersions(Version, info.TagName) < 0 {
			ch <- info
		}
	}()
	return ch
}

// PrintUpdateBanner prints a bordered update notice to stderr.
func PrintUpdateBanner(latest *ReleaseInfo) {
	line1 := fmt.Sprintf("  Update available! %s -> %s", Version, latest.TagName)
	line2 := "  Run with --update to upgrade"

	width := len(line1)
	if len(line2) > width {
		width = len(line2)
	}
	width += 2 // padding on the right

	border := strings.Repeat("\u2550", width)
	pad1 := strings.Repeat(" ", width-len(line1))
	pad2 := strings.Repeat(" ", width-len(line2))

	fmt.Fprintf(nativeStderr, "\033[33m")
	fmt.Fprintf(nativeStderr, "\u2554%s\u2557\n", border)
	fmt.Fprintf(nativeStderr, "\u2551%s%s\u2551\n", line1, pad1)
	fmt.Fprintf(nativeStderr, "\u2551%s%s\u2551\n", line2, pad2)
	fmt.Fprintf(nativeStderr, "\u255a%s\u255d\n", border)
	fmt.Fprintf(nativeStderr, "\033[0m")
}
