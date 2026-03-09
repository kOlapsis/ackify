// SPDX-License-Identifier: AGPL-3.0-or-later
package storage

import (
	"fmt"

	"github.com/kolapsis/ackify/backend/pkg/config"
)

func NewProvider(cfg config.StorageConfig) (Provider, error) {
	if !cfg.IsEnabled() {
		return nil, nil
	}

	switch cfg.Type {
	case "local":
		return NewLocalProvider(cfg.LocalPath)
	case "s3":
		return NewS3Provider(S3Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Region:    cfg.S3Region,
			UseSSL:    cfg.S3UseSSL,
		})
	default:
		return nil, fmt.Errorf("unknown storage type: %s", cfg.Type)
	}
}
