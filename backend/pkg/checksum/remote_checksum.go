// SPDX-License-Identifier: AGPL-3.0-or-later
package checksum

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kolapsis/ackify/backend/pkg/logger"
)

// ChecksumResult represents the result of a checksum computation
type Result struct {
	ChecksumHex string
	Algorithm   string
}

// ComputeOptions configures the remote checksum computation behavior
type ComputeOptions struct {
	MaxBytes           int64
	TimeoutMs          int
	MaxRedirects       int
	AllowedContentType []string
	SkipSSRFCheck      bool // For testing only - disables SSRF protection
	InsecureSkipVerify bool // For testing only - disables TLS verification
}

// DefaultOptions returns the default configuration for checksum computation
func DefaultOptions() ComputeOptions {
	return ComputeOptions{
		MaxBytes:     10 * 1024 * 1024, // 10 MB
		TimeoutMs:    5000,             // 5 seconds
		MaxRedirects: 3,
		AllowedContentType: []string{
			"application/pdf",
			"image/jpeg",
			"image/png",
			"image/gif",
			"image/webp",
			"image/svg+xml",
			"application/msword",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			"application/vnd.ms-excel",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
			"application/vnd.oasis.opendocument.text",
			"application/vnd.oasis.opendocument.spreadsheet",
			"application/vnd.oasis.opendocument.presentation",
		},
	}
}

// ComputeRemoteChecksum downloads a remote binary file and computes its SHA-256 checksum
// Returns nil if the file cannot be processed (too large, wrong type, network error, SSRF blocked)
// The context is used for request cancellation and timeout propagation.
func ComputeRemoteChecksum(ctx context.Context, urlStr string, opts ComputeOptions) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("context cancelled before checksum computation: %w", err)
	}

	if !isValidURL(urlStr) {
		logger.Logger.Info("Checksum: URL rejected - not HTTPS", "url", urlStr)
		return nil, nil
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		logger.Logger.Warn("Checksum: Failed to parse URL", "url", urlStr, "error", err.Error())
		return nil, nil
	}

	if !opts.SkipSSRFCheck && isBlockedHost(parsedURL.Hostname()) {
		logger.Logger.Warn("Checksum: SSRF protection - blocked internal/private host", "host", parsedURL.Hostname())
		return nil, nil
	}

	client := &http.Client{
		Timeout: time.Duration(opts.TimeoutMs) * time.Millisecond,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= opts.MaxRedirects {
				return fmt.Errorf("too many redirects")
			}
			// SSRF protection on redirects (unless disabled for testing)
			if !opts.SkipSSRFCheck && isBlockedHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to blocked host: %s", req.URL.Hostname())
			}
			return nil
		},
	}

	if opts.InsecureSkipVerify {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
	}

	// Step 1: HEAD request to check Content-Type and Content-Length
	headReq, err := http.NewRequestWithContext(ctx, "HEAD", urlStr, nil)
	if err != nil {
		logger.Logger.Warn("Checksum: Failed to create HEAD request", "url", urlStr, "error", err.Error())
		return nil, nil
	}
	headReq.Header.Set("User-Agent", "Ackify-Checksum/1.0")

	headResp, err := client.Do(headReq)
	if err != nil {
		logger.Logger.Info("Checksum: HEAD request failed", "url", urlStr, "error", err.Error())
		// Fallback: try GET with streaming if HEAD not supported
		return computeWithStreamedGET(ctx, client, urlStr, opts)
	}
	defer headResp.Body.Close()

	contentType := headResp.Header.Get("Content-Type")
	if contentType != "" && !isAllowedContentType(contentType, opts.AllowedContentType) {
		logger.Logger.Info("Checksum: Content-Type not allowed", "url", urlStr, "content_type", contentType)
		return nil, nil
	}

	contentLength := headResp.ContentLength
	if contentLength > 0 && contentLength > opts.MaxBytes {
		logger.Logger.Info("Checksum: File too large", "url", urlStr, "size", contentLength, "max", opts.MaxBytes)
		return nil, nil
	}

	// If Content-Length is unknown (0 or -1), fallback to streamed GET
	if contentLength <= 0 {
		logger.Logger.Debug("Checksum: Content-Length unknown, using streamed GET", "url", urlStr)
		return computeWithStreamedGET(ctx, client, urlStr, opts)
	}

	// Step 2: GET request to download and compute checksum
	getReq, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		logger.Logger.Warn("Checksum: Failed to create GET request", "url", urlStr, "error", err.Error())
		return nil, nil
	}
	getReq.Header.Set("User-Agent", "Ackify-Checksum/1.0")

	getResp, err := client.Do(getReq)
	if err != nil {
		logger.Logger.Info("Checksum: GET request failed", "url", urlStr, "error", err.Error())
		return nil, nil
	}
	defer getResp.Body.Close()

	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		logger.Logger.Info("Checksum: HTTP error", "url", urlStr, "status", getResp.StatusCode)
		return nil, nil
	}

	// Compute SHA-256 with size limit
	return computeHashWithLimit(getResp.Body, opts.MaxBytes, urlStr)
}

// computeWithStreamedGET performs a GET request and computes checksum with hard size limit
func computeWithStreamedGET(ctx context.Context, client *http.Client, urlStr string, opts ComputeOptions) (*Result, error) {
	getReq, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		logger.Logger.Warn("Checksum: Failed to create GET request (fallback)", "url", urlStr, "error", err.Error())
		return nil, nil
	}
	getReq.Header.Set("User-Agent", "Ackify-Checksum/1.0")

	getResp, err := client.Do(getReq)
	if err != nil {
		logger.Logger.Info("Checksum: GET request failed (fallback)", "url", urlStr, "error", err.Error())
		return nil, nil
	}
	defer getResp.Body.Close()

	if getResp.StatusCode < 200 || getResp.StatusCode >= 300 {
		logger.Logger.Info("Checksum: HTTP error (fallback)", "url", urlStr, "status", getResp.StatusCode)
		return nil, nil
	}

	contentType := getResp.Header.Get("Content-Type")
	if contentType != "" && !isAllowedContentType(contentType, opts.AllowedContentType) {
		logger.Logger.Info("Checksum: Content-Type not allowed (fallback)", "url", urlStr, "content_type", contentType)
		return nil, nil
	}

	return computeHashWithLimit(getResp.Body, opts.MaxBytes, urlStr)
}

// computeHashWithLimit computes SHA-256 hash with a hard size limit
func computeHashWithLimit(reader io.Reader, maxBytes int64, urlStr string) (*Result, error) {
	hasher := sha256.New()
	limitedReader := io.LimitReader(reader, maxBytes+1) // +1 to detect overflow

	written, err := io.Copy(hasher, limitedReader)
	if err != nil {
		logger.Logger.Warn("Checksum: Failed to read stream", "url", urlStr, "error", err.Error())
		return nil, nil
	}

	if written > maxBytes {
		logger.Logger.Info("Checksum: File exceeded size limit during streaming", "url", urlStr, "read", written, "max", maxBytes)
		return nil, nil
	}

	checksumHex := hex.EncodeToString(hasher.Sum(nil))
	logger.Logger.Info("Checksum: Successfully computed", "url", urlStr, "checksum", checksumHex, "bytes", written)

	return &Result{
		ChecksumHex: checksumHex,
		Algorithm:   "SHA-256",
	}, nil
}

// isValidURL checks if the URL uses HTTPS scheme
func isValidURL(urlStr string) bool {
	return strings.HasPrefix(strings.ToLower(urlStr), "https://")
}

// isAllowedContentType checks if the content type is in the allowed list
func isAllowedContentType(contentType string, allowedTypes []string) bool {
	// Extract the base type (before ';' for charset/boundary)
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))

	for _, allowed := range allowedTypes {
		allowedLower := strings.ToLower(allowed)
		// Exact match
		if contentType == allowedLower {
			return true
		}
		// Wildcard match (e.g., "image/*", "application/vnd.oasis.opendocument.*")
		if strings.HasSuffix(allowedLower, "/*") {
			prefix := strings.TrimSuffix(allowedLower, "/*")
			if strings.HasPrefix(contentType, prefix+"/") {
				return true
			}
		}
		// Pattern match with * at the end (e.g., "application/vnd.oasis.opendocument.*")
		if strings.HasSuffix(allowedLower, ".*") {
			prefix := strings.TrimSuffix(allowedLower, ".*")
			if strings.HasPrefix(contentType, prefix+".") {
				return true
			}
		}
	}

	return false
}

// isBlockedHost checks if the hostname is a private/internal IP or localhost
func isBlockedHost(hostname string) bool {
	if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
		return true
	}

	// Try to resolve the IP
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If we can't resolve, be conservative and block it
		logger.Logger.Warn("Checksum: Failed to resolve hostname", "hostname", hostname, "error", err.Error())
		return true
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return true
		}
	}

	return false
}

// isPrivateIP checks if an IP is in a private/reserved range
func isPrivateIP(ip net.IP) bool {
	// Private IPv4 ranges
	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",        // Loopback
		"169.254.0.0/16",     // Link-local
		"224.0.0.0/4",        // Multicast
		"240.0.0.0/4",        // Reserved
		"0.0.0.0/8",          // Current network
		"100.64.0.0/10",      // Shared Address Space (RFC 6598)
		"192.0.0.0/24",       // IETF Protocol Assignments
		"192.0.2.0/24",       // TEST-NET-1
		"198.18.0.0/15",      // Benchmarking
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"255.255.255.255/32", // Broadcast
	}

	for _, cidr := range privateRanges {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return true
		}
	}

	if ip.To4() == nil {
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return true
		}
		// Unique Local Addresses (ULA) - fc00::/7
		if len(ip) >= 1 && (ip[0]&0xfe) == 0xfc {
			return true
		}
	}

	return false
}
