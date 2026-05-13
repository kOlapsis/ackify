// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import (
	"net/url"
	"strings"
)

// SafeRedirectURL validates rawURL for use as a post-auth redirect target.
// By default only same-origin relative paths are accepted. If allowedHosts is
// non-empty, absolute URLs whose host matches an entry in the list are also
// permitted, enabling cross-origin redirects to known partner domains.
// Any URL that does not pass validation falls back to "/".
func SafeRedirectURL(rawURL string, allowedHosts []string) string {
	if rawURL == "" {
		return "/"
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "/"
	}

	// Absolute URL: only allowed when host is in the explicit allowlist.
	if u.Scheme != "" || u.Host != "" {
		if len(allowedHosts) > 0 && isAllowedRedirectHost(u.Host, allowedHosts) {
			return rawURL
		}
		return "/"
	}

	// Relative URL: must start with / but not // (protocol-relative).
	// Backslash check is critical: browsers normalize /\evil.com → //evil.com.
	if !strings.HasPrefix(u.Path, "/") ||
		strings.HasPrefix(u.Path, "//") ||
		strings.ContainsRune(rawURL, '\\') {
		return "/"
	}

	return rawURL
}

func isAllowedRedirectHost(host string, allowedHosts []string) bool {
	for _, entry := range allowedHosts {
		if strings.EqualFold(host, strings.TrimSpace(entry)) {
			return true
		}
	}
	return false
}
