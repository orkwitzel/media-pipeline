package main

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Minio struct {
	client  *minio.Client
	buckets []string
}

func NewMinio(c Config) (*Minio, error) {
	client, err := minio.New(c.S3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.S3Access, c.S3Secret, ""),
		Secure: c.S3UseSSL,
		Region: c.S3Region,
	})
	if err != nil {
		return nil, fmt.Errorf("minio.New: %w", err)
	}
	return &Minio{client: client, buckets: []string{c.BucketOriginals, c.BucketProcessed}}, nil
}

func (m *Minio) EnsureBuckets(ctx context.Context) error {
	for _, b := range m.buckets {
		exists, err := m.client.BucketExists(ctx, b)
		if err != nil {
			return fmt.Errorf("BucketExists(%s): %w", b, err)
		}
		if !exists {
			if err := m.client.MakeBucket(ctx, b, minio.MakeBucketOptions{}); err != nil {
				return fmt.Errorf("MakeBucket(%s): %w", b, err)
			}
		}
	}
	return nil
}

func (m *Minio) Put(ctx context.Context, bucket, key, contentType string, b []byte) error {
	_, err := m.client.PutObject(ctx, bucket, key, bytes.NewReader(b), int64(len(b)),
		minio.PutObjectOptions{ContentType: contentType})
	return err
}

func (m *Minio) Get(ctx context.Context, bucket, key string) ([]byte, string, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	defer obj.Close()
	info, err := obj.Stat()
	if err != nil {
		return nil, "", err
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", err
	}
	return data, info.ContentType, nil
}
