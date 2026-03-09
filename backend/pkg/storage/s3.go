// SPDX-License-Identifier: AGPL-3.0-or-later
package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kolapsis/ackify/backend/pkg/logger"
)

type S3Provider struct {
	client *s3.Client
	bucket string
	useSSL bool
}

type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

func NewS3Provider(cfg S3Config) (*S3Provider, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	// Build AWS config
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	// Use custom credentials if provided
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with optional custom endpoint
	s3Opts := []func(*s3.Options){}
	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for MinIO and most S3-compatible services
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	// Check if bucket exists, create if not
	_, err = client.HeadBucket(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		// Try to create the bucket
		_, createErr := client.CreateBucket(context.Background(), &s3.CreateBucketInput{
			Bucket: aws.String(cfg.Bucket),
		})
		if createErr != nil {
			return nil, fmt.Errorf("bucket %s does not exist and failed to create it: %w", cfg.Bucket, createErr)
		}
		logger.Logger.Info("S3 bucket created", "bucket", cfg.Bucket)
	}

	logger.Logger.Info("S3 storage provider initialized", "bucket", cfg.Bucket, "endpoint", cfg.Endpoint)

	return &S3Provider{
		client: client,
		bucket: cfg.Bucket,
		useSSL: cfg.UseSSL,
	}, nil
}

func (p *S3Provider) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	var body io.Reader = reader
	contentLength := size

	// When not using TLS, AWS SDK v2 requires a seekable stream for checksum computation.
	// Read content into memory to create a seekable bytes.Reader.
	if !p.useSSL {
		data, err := io.ReadAll(reader)
		if err != nil {
			return fmt.Errorf("failed to read content for upload: %w", err)
		}
		body = bytes.NewReader(data)
		contentLength = int64(len(data))
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(p.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	}

	if contentLength > 0 {
		input.ContentLength = aws.Int64(contentLength)
	}

	_, err := p.client.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	logger.Logger.Debug("File uploaded to S3", "key", key, "bucket", p.bucket)
	return nil
}

func (p *S3Provider) Download(ctx context.Context, key string) (io.ReadCloser, int64, string, error) {
	output, err := p.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("failed to download from S3: %w", err)
	}

	var size int64
	if output.ContentLength != nil {
		size = *output.ContentLength
	}

	contentType := "application/octet-stream"
	if output.ContentType != nil {
		contentType = *output.ContentType
	}

	return output.Body, size, contentType, nil
}

func (p *S3Provider) Delete(ctx context.Context, key string) error {
	_, err := p.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	logger.Logger.Debug("File deleted from S3", "key", key, "bucket", p.bucket)
	return nil
}

func (p *S3Provider) Exists(ctx context.Context, key string) (bool, error) {
	_, err := p.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(p.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		// Check if it's a "not found" error
		return false, nil
	}
	return true, nil
}

func (p *S3Provider) Type() string {
	return "s3"
}
