package backup

import (
	"fmt"
	"regexp"
	"strconv"
)

// version is a parsed restic semantic version.
type version struct {
	major, minor, patch int
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v version) olderThan(other version) bool {
	if v.major != other.major {
		return v.major < other.major
	}
	if v.minor != other.minor {
		return v.minor < other.minor
	}
	return v.patch < other.patch
}

// resticVersionPattern matches the version in restic's `version` output, e.g.
// "restic 0.19.1 compiled with go1.26.5 on darwin/arm64".
var resticVersionPattern = regexp.MustCompile(`restic\s+v?(\d+)\.(\d+)(?:\.(\d+))?`)

func parseResticVersion(output string) (version, error) {
	match := resticVersionPattern.FindStringSubmatch(output)
	if match == nil {
		return version{}, fmt.Errorf("no restic version found in %q", output)
	}

	major, _ := strconv.Atoi(match[1])
	minor, _ := strconv.Atoi(match[2])
	patch := 0
	if match[3] != "" {
		patch, _ = strconv.Atoi(match[3])
	}
	return version{major, minor, patch}, nil
}
