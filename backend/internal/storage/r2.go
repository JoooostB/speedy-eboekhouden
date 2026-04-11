package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Config holds R2 connection settings.
type Config struct {
	AccountID       string // Cloudflare account ID
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	Jurisdiction    string // "eu" for EU data residency, empty for default
	PublicURL       string // e.g. https://cdn.speedy-eboekhouden.nl
}

// Client wraps an S3-compatible client for Cloudflare R2.
type Client struct {
	s3        *s3.Client
	bucket    string
	publicURL string
}

// New creates a new R2 storage client. Returns nil if not configured.
func New(cfg Config) *Client {
	if cfg.AccountID == "" || cfg.AccessKeyID == "" || cfg.BucketName == "" {
		return nil
	}

	// EU jurisdiction uses a different endpoint subdomain
	var endpoint string
	if cfg.Jurisdiction == "eu" {
		endpoint = fmt.Sprintf("https://%s.eu.r2.cloudflarestorage.com", cfg.AccountID)
	} else {
		endpoint = fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.AccountID)
	}

	s3Client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: &endpoint,
		Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
	})

	return &Client{s3: s3Client, bucket: cfg.BucketName, publicURL: cfg.PublicURL}
}

// Upload stores a file in R2. Returns the object key.
func (c *Client) Upload(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &c.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("uploading to R2: %w", err)
	}
	return nil
}

// Download retrieves a file from R2.
func (c *Client) Download(ctx context.Context, key string) ([]byte, string, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, "", fmt.Errorf("downloading from R2: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading R2 object: %w", err)
	}

	ct := "application/octet-stream"
	if out.ContentType != nil {
		ct = *out.ContentType
	}
	return data, ct, nil
}

// Delete removes a file from R2.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return fmt.Errorf("deleting from R2: %w", err)
	}
	return nil
}

// PublicObjectURL returns the public CDN URL for a key (if R2_PUBLIC_URL is set).
func (c *Client) PublicObjectURL(key string) string {
	if c.publicURL == "" {
		return ""
	}
	return c.publicURL + "/" + key
}

// GeneratePresignedURL creates a pre-signed URL with a 15-minute TTL.
func (c *Client) GeneratePresignedURL(ctx context.Context, key string) (string, error) {
	presigner := s3.NewPresignClient(c.s3, func(o *s3.PresignOptions) {
		o.Expires = 15 * time.Minute
	})
	req, err := presigner.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &c.bucket,
		Key:    &key,
	})
	if err != nil {
		return "", fmt.Errorf("generating presigned URL: %w", err)
	}
	return req.URL, nil
}
