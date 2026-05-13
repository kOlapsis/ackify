// SPDX-License-Identifier: AGPL-3.0-or-later
package shared

import "testing"

func TestSafeRedirectURL(t *testing.T) {
	tests := []struct {
		name         string
		rawURL       string
		allowedHosts []string
		want         string
	}{
		// Safe relative paths
		{name: "empty falls back to root", rawURL: "", want: "/"},
		{name: "root path", rawURL: "/", want: "/"},
		{name: "sub-path", rawURL: "/dashboard", want: "/dashboard"},
		{name: "path with query", rawURL: "/docs?id=42", want: "/docs?id=42"},
		{name: "path with fragment", rawURL: "/page#section", want: "/page#section"},
		{name: "deep path", rawURL: "/a/b/c", want: "/a/b/c"},

		// Backslash bypass (the reported CVE pattern)
		{name: "backslash bypass root", rawURL: `/\evil.com`, want: "/"},
		{name: "backslash bypass path", rawURL: `/\evil.com/foo`, want: "/"},
		{name: "double backslash", rawURL: `\\evil.com`, want: "/"},

		// Protocol-relative bypass
		{name: "protocol-relative //", rawURL: "//evil.com", want: "/"},
		{name: "protocol-relative // with path", rawURL: "//evil.com/steal", want: "/"},

		// Absolute URLs without allowlist
		{name: "http scheme rejected", rawURL: "http://evil.com", want: "/"},
		{name: "https scheme rejected", rawURL: "https://evil.com", want: "/"},
		{name: "javascript scheme", rawURL: "javascript:alert(1)", want: "/"},

		// Absolute URLs with allowlist — matching host
		{name: "allowed host exact match", rawURL: "https://partner.com/path", allowedHosts: []string{"partner.com"}, want: "https://partner.com/path"},
		{name: "allowed host with port", rawURL: "https://app.example.com:8443/x", allowedHosts: []string{"app.example.com:8443"}, want: "https://app.example.com:8443/x"},
		{name: "allowed host case-insensitive", rawURL: "https://Partner.Com/p", allowedHosts: []string{"partner.com"}, want: "https://Partner.Com/p"},

		// Absolute URLs with allowlist — non-matching host
		{name: "unlisted host rejected", rawURL: "https://evil.com", allowedHosts: []string{"partner.com"}, want: "/"},
		{name: "subdomain not in list", rawURL: "https://sub.partner.com", allowedHosts: []string{"partner.com"}, want: "/"},

		// Edge cases
		{name: "path not starting with slash", rawURL: "relative/path", want: "/"},
		{name: "empty allowlist with absolute URL", rawURL: "https://example.com", allowedHosts: []string{}, want: "/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SafeRedirectURL(tt.rawURL, tt.allowedHosts)
			if got != tt.want {
				t.Errorf("SafeRedirectURL(%q, %v) = %q, want %q", tt.rawURL, tt.allowedHosts, got, tt.want)
			}
		})
	}
}
