// SPDX-License-Identifier: AGPL-3.0-or-later
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kolapsis/ackify/backend/pkg/logger"
)

type LocalProvider struct {
	basePath string
}

func NewLocalProvider(basePath string) (*LocalProvider, error) {
	// Clean and validate the path
	cleanPath := filepath.Clean(basePath)

	// Ensure directory exists with proper permissions
	if err := os.MkdirAll(cleanPath, 0750); err != nil {
		return nil, fmt.Errorf("failed to create storage directory %s: %w", cleanPath, err)
	}

	// Verify directory is writable
	testFile := filepath.Join(cleanPath, ".write_test")
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		return nil, fmt.Errorf("storage directory %s is not writable: %w", cleanPath, err)
	}
	os.Remove(testFile)

	logger.Logger.Info("Local storage provider initialized", "path", cleanPath)

	return &LocalProvider{
		basePath: cleanPath,
	}, nil
}

func (p *LocalProvider) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	// Sanitize key to prevent path traversal
	safeKey := sanitizeKey(key)
	if safeKey == "" {
		return fmt.Errorf("invalid storage key")
	}

	fullPath := filepath.Join(p.basePath, safeKey)

	// Ensure parent directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Use atomic write: write to temp file then rename
	tempFile := fullPath + ".tmp." + randomSuffix()

	file, err := os.OpenFile(tempFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	// Ensure cleanup on error
	success := false
	defer func() {
		if !success {
			file.Close()
			os.Remove(tempFile)
		}
	}()

	// Copy data with context cancellation check
	written, err := copyWithContext(ctx, file, reader)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	if size > 0 && written != size {
		return fmt.Errorf("size mismatch: expected %d, wrote %d", size, written)
	}

	// Sync to ensure data is on disk
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %w", err)
	}

	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempFile, fullPath); err != nil {
		return fmt.Errorf("failed to finalize file: %w", err)
	}

	success = true
	logger.Logger.Debug("File uploaded to local storage", "key", safeKey, "size", written)

	return nil
}

func (p *LocalProvider) Download(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	safeKey := sanitizeKey(key)
	if safeKey == "" {
		return nil, 0, "", fmt.Errorf("invalid storage key")
	}

	fullPath := filepath.Join(p.basePath, safeKey)

	// Verify path is still within basePath (double-check)
	if !strings.HasPrefix(fullPath, p.basePath) {
		return nil, 0, "", fmt.Errorf("invalid storage key: path traversal detected")
	}

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, "", fmt.Errorf("file not found: %s", key)
		}
		return nil, 0, "", fmt.Errorf("failed to open file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, 0, "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Detect content type from first 512 bytes
	buffer := make([]byte, 512)
	n, _ := file.Read(buffer)
	contentType := http.DetectContentType(buffer[:n])

	// Reset file position
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, 0, "", fmt.Errorf("failed to seek file: %w", err)
	}

	return file, stat.Size(), contentType, nil
}

func (p *LocalProvider) Delete(ctx context.Context, key string) error {
	safeKey := sanitizeKey(key)
	if safeKey == "" {
		return fmt.Errorf("invalid storage key")
	}

	fullPath := filepath.Join(p.basePath, safeKey)

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete file: %w", err)
	}

	logger.Logger.Debug("File deleted from local storage", "key", safeKey)
	return nil
}

func (p *LocalProvider) Exists(ctx context.Context, key string) (bool, error) {
	safeKey := sanitizeKey(key)
	if safeKey == "" {
		return false, fmt.Errorf("invalid storage key")
	}

	fullPath := filepath.Join(p.basePath, safeKey)

	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check file: %w", err)
}

func (p *LocalProvider) Type() string {
	return "local"
}

// sanitizeKey removes any path traversal attempts and ensures safe key
func sanitizeKey(key string) string {
	// Remove any leading/trailing whitespace
	key = strings.TrimSpace(key)

	// Remove any path traversal attempts
	key = strings.ReplaceAll(key, "..", "")
	key = strings.ReplaceAll(key, "//", "/")

	// Clean the path
	key = filepath.Clean(key)

	// Remove leading slash
	key = strings.TrimPrefix(key, "/")

	// Ensure no remaining path traversal
	if strings.Contains(key, "..") {
		return ""
	}

	return key
}

// randomSuffix generates a random suffix for temp files
func randomSuffix() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// copyWithContext copies data while checking for context cancellation
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64

	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = fmt.Errorf("invalid write result")
				}
			}
			written += int64(nw)
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er != io.EOF {
				return written, er
			}
			break
		}
	}
	return written, nil
}
