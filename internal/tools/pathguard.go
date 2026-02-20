package tools

import (
	"fmt"
	"path/filepath"
	"strings"
)

// blockedDirs are directory names that are always blocked.
var blockedDirs = []string{".ssh", ".aws", ".gnupg", ".kube"}

// blockedFiles are file basenames that are always blocked.
var blockedFiles = []string{
	".env", ".env.local", ".env.production", ".env.development",
	"credentials", "credentials.json", "token.json",
	"secrets.yaml", "secrets.json",
	"id_rsa", "id_rsa.pub", "id_ed25519", "id_ed25519.pub",
	"id_ecdsa", "id_ecdsa.pub", "id_dsa", "id_dsa.pub",
	".netrc", ".npmrc",
}

// blockedAbsolute are full paths that are always blocked.
var blockedAbsolute = []string{"/etc/shadow", "/etc/gshadow", "/etc/sudoers"}

// blockedSubpaths are path segments that are blocked when found as subpaths.
var blockedSubpaths = []string{".config/gcloud"}

// CheckPath returns an error if the path is blocked by security rules.
func CheckPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	normalized := filepath.Clean(abs)

	// Check blocked absolute paths.
	for _, bp := range blockedAbsolute {
		if normalized == bp {
			return fmt.Errorf("access to %s is blocked for security reasons", path)
		}
	}

	// Check blocked directories.
	parts := strings.Split(normalized, string(filepath.Separator))
	for _, part := range parts {
		for _, bd := range blockedDirs {
			if part == bd {
				return fmt.Errorf("access to %s is blocked for security reasons (sensitive directory: %s)", path, bd)
			}
		}
	}

	// Check blocked subpaths.
	for _, sp := range blockedSubpaths {
		if strings.Contains(normalized, sp) {
			return fmt.Errorf("access to %s is blocked for security reasons", path)
		}
	}

	// Check blocked file basenames.
	base := filepath.Base(normalized)
	for _, bf := range blockedFiles {
		if base == bf {
			return fmt.Errorf("access to %s is blocked for security reasons (sensitive file: %s)", path, bf)
		}
	}

	return nil
}

// IsBlocked returns true if the path is blocked.
func IsBlocked(path string) bool {
	return CheckPath(path) != nil
}

// FilterPaths removes blocked paths from a list.
func FilterPaths(paths []string) []string {
	var result []string
	for _, p := range paths {
		if !IsBlocked(p) {
			result = append(result, p)
		}
	}
	return result
}
