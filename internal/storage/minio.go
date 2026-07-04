package storage

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioClient struct {
	client *minio.Client
}

func NewMinioClient(endpoint, accessKey, secretKey string, useSSL bool) (*MinioClient, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("minio connect: %w", err)
	}
	log.Println("connected to MinIO at", endpoint)
	return &MinioClient{client: client}, nil
}

func (m *MinioClient) EnsureBucket(ctx context.Context, name string) error {
	exists, err := m.client.BucketExists(ctx, name)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", name, err)
	}
	if !exists {
		if err := m.client.MakeBucket(ctx, name, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("create bucket %s: %w", name, err)
		}
		log.Printf("created MinIO bucket: %s", name)
	}
	return nil
}

func (m *MinioClient) Upload(ctx context.Context, bucket, key string, reader io.Reader, size int64, contentType string) error {
	_, err := m.client.PutObject(ctx, bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("upload %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (m *MinioClient) Download(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", bucket, key, err)
	}
	return obj, nil
}

func (m *MinioClient) Delete(ctx context.Context, bucket, key string) error {
	err := m.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("delete %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (m *MinioClient) GetObjectInfo(ctx context.Context, bucket, key string) (minio.ObjectInfo, error) {
	return m.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
}

func PersonalBucket(userID int) string {
	return fmt.Sprintf("personal-%d", userID)
}

func ProjectBucket(projectID int) string {
	return fmt.Sprintf("project-%d", projectID)
}
