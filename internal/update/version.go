package update

import (
	"strconv"
	"strings"
)

// Version is set at build time via -ldflags "-X localsync/internal/update.Version=vX.Y.Z"
var Version = "dev"

// CompareVersions compares two semver strings (with or without "v" prefix).
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func CompareVersions(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		// Strip anything after a hyphen (e.g. "1.0.0-beta")
		clean := strings.SplitN(parts[i], "-", 2)[0]
		result[i], _ = strconv.Atoi(clean)
	}
	return result
}
