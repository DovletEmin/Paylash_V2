package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioClient struct {
	client *minio.Client
	core   *minio.Core
	// publicClient signs URLs against a host:port the BROWSER can resolve
	// (PAYLASH_MINIO_PUBLIC_ENDPOINT) — nil when that's not configured, in
	// which case large-file resumable upload is unavailable. client itself
	// keeps using the internal Docker-network endpoint for everything else.
	publicClient *minio.Client
}

// minioRegion is passed explicitly to every client so the SDK never needs to
// call GetBucketLocation to auto-detect it (MinIO defaults to this region
// anyway). That auto-detection call matters in particular for publicClient:
// it's constructed with a host:port only the BROWSER can reach, so if the
// SDK tried to dial it from inside the app container to look up the region,
// it would simply fail to connect.
const minioRegion = "us-east-1"

func NewMinioClient(endpoint, accessKey, secretKey string, useSSL bool, publicEndpoint string) (*MinioClient, error) {
	creds := credentials.NewStaticV4(accessKey, secretKey, "")
	client, err := minio.New(endpoint, &minio.Options{Creds: creds, Secure: useSSL, Region: minioRegion})
	if err != nil {
		return nil, fmt.Errorf("minio connect: %w", err)
	}
	log.Println("connected to MinIO at", endpoint)

	mc := &MinioClient{client: client, core: &minio.Core{Client: client}}

	if publicEndpoint != "" {
		pubClient, err := minio.New(publicEndpoint, &minio.Options{Creds: creds, Secure: useSSL, Region: minioRegion})
		if err != nil {
			return nil, fmt.Errorf("minio public client: %w", err)
		}
		mc.publicClient = pubClient
	} else {
		log.Println("PAYLASH_MINIO_PUBLIC_ENDPOINT not set — large-file resumable upload disabled")
	}
	return mc, nil
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
	// Idempotent — also retroactively enables versioning on buckets that
	// existed before this feature, the next time anything touches them.
	if err := m.EnableVersioning(ctx, name); err != nil {
		return err
	}
	return nil
}

// EnableVersioning turns on bucket versioning, so every overwrite (Collabora
// autosave, re-upload) keeps its previous content as a retrievable version
// instead of being lost.
func (m *MinioClient) EnableVersioning(ctx context.Context, bucket string) error {
	if err := m.client.EnableVersioning(ctx, bucket); err != nil {
		return fmt.Errorf("enable versioning on %s: %w", bucket, err)
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

// Download returns a seekable, streaming handle to the object. Callers can
// pass it straight to http.ServeContent (Range/seek requests are proxied to
// MinIO on demand) instead of buffering the whole object in memory first.
func (m *MinioClient) Download(ctx context.Context, bucket, key string) (io.ReadSeekCloser, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("download %s/%s: %w", bucket, key, err)
	}
	return obj, nil
}

// Delete permanently removes an object, including every historical version —
// with bucket versioning on, a plain unversioned RemoveObject would only
// drop a delete marker on top and leak the underlying data forever. All
// current callers use Delete for genuine permanent removal (trash purge),
// so that's the semantics this provides.
func (m *MinioClient) Delete(ctx context.Context, bucket, key string) error {
	for obj := range m.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key, WithVersions: true}) {
		if obj.Err != nil {
			return fmt.Errorf("list versions of %s/%s: %w", bucket, key, obj.Err)
		}
		if obj.Key != key {
			continue
		}
		if err := m.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{VersionID: obj.VersionID}); err != nil {
			return fmt.Errorf("delete %s/%s (version %s): %w", bucket, key, obj.VersionID, err)
		}
	}
	return nil
}

// RemoveBucketAndAllObjects deletes every object (all versions) in bucket
// and then the bucket itself. It's meant for buckets that are wholly owned
// by a single project or user (project-{id}, personal-{id}) — never call it
// on a shared bucket like common-files. A missing bucket is not an error.
func (m *MinioClient) RemoveBucketAndAllObjects(ctx context.Context, bucket string) error {
	exists, err := m.client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	if !exists {
		return nil
	}
	for obj := range m.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
		if obj.Err != nil {
			return fmt.Errorf("list objects in %s: %w", bucket, obj.Err)
		}
		if err := m.client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{VersionID: obj.VersionID}); err != nil {
			return fmt.Errorf("remove object %s/%s (version %s): %w", bucket, obj.Key, obj.VersionID, err)
		}
	}
	if err := m.client.RemoveBucket(ctx, bucket); err != nil {
		return fmt.Errorf("remove bucket %s: %w", bucket, err)
	}
	return nil
}

// ObjectVersion describes one historical version of an object.
type ObjectVersion struct {
	VersionID      string    `json:"version_id"`
	Size           int64     `json:"size_bytes"`
	LastModified   time.Time `json:"last_modified"`
	IsLatest       bool      `json:"is_latest"`
	IsDeleteMarker bool      `json:"-"`
}

// ListVersions returns every version of a single object, newest first.
func (m *MinioClient) ListVersions(ctx context.Context, bucket, key string) ([]ObjectVersion, error) {
	var versions []ObjectVersion
	for obj := range m.client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: key, WithVersions: true}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list versions of %s/%s: %w", bucket, key, obj.Err)
		}
		if obj.Key != key || obj.IsDeleteMarker {
			continue
		}
		versions = append(versions, ObjectVersion{
			VersionID:    obj.VersionID,
			Size:         obj.Size,
			LastModified: obj.LastModified,
			IsLatest:     obj.IsLatest,
		})
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].LastModified.After(versions[j].LastModified) })
	return versions, nil
}

// RestoreVersion copies an old version back on top of the current object,
// creating a new version at the top of the history — nothing is lost.
func (m *MinioClient) RestoreVersion(ctx context.Context, bucket, key, versionID string) error {
	src := minio.CopySrcOptions{Bucket: bucket, Object: key, VersionID: versionID}
	dst := minio.CopyDestOptions{Bucket: bucket, Object: key}
	if _, err := m.client.CopyObject(ctx, dst, src); err != nil {
		return fmt.Errorf("restore version %s of %s/%s: %w", versionID, bucket, key, err)
	}
	return nil
}

// DownloadVersion streams one specific historical version of an object.
func (m *MinioClient) DownloadVersion(ctx context.Context, bucket, key, versionID string) (io.ReadSeekCloser, error) {
	obj, err := m.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{VersionID: versionID})
	if err != nil {
		return nil, fmt.Errorf("download version %s of %s/%s: %w", versionID, bucket, key, err)
	}
	return obj, nil
}

// RemoveVersion permanently deletes one specific historical version —
// used by the janitor to trim versions past the retention window.
func (m *MinioClient) RemoveVersion(ctx context.Context, bucket, key, versionID string) error {
	if err := m.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{VersionID: versionID}); err != nil {
		return fmt.Errorf("remove version %s of %s/%s: %w", versionID, bucket, key, err)
	}
	return nil
}

func (m *MinioClient) GetObjectInfo(ctx context.Context, bucket, key string) (minio.ObjectInfo, error) {
	return m.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
}

// GetVersionInfo stats one specific historical version — used to get its
// real LastModified for conditional-GET support when downloading it
// (DownloadVersion returns a plain io.ReadSeekCloser with no Stat method).
func (m *MinioClient) GetVersionInfo(ctx context.Context, bucket, key, versionID string) (minio.ObjectInfo, error) {
	return m.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{VersionID: versionID})
}

// InitMultipartUpload begins a new multipart upload and returns its upload ID.
func (m *MinioClient) InitMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error) {
	uploadID, err := m.core.NewMultipartUpload(ctx, bucket, key, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", fmt.Errorf("init multipart upload %s/%s: %w", bucket, key, err)
	}
	return uploadID, nil
}

// PresignPartUpload signs a URL the browser can PUT one part's bytes to
// directly, bypassing the app server entirely for the actual data transfer.
// Requires PAYLASH_MINIO_PUBLIC_ENDPOINT to be configured.
func (m *MinioClient) PresignPartUpload(ctx context.Context, bucket, key, uploadID string, partNumber int, expiry time.Duration) (string, error) {
	if m.publicClient == nil {
		return "", fmt.Errorf("PAYLASH_MINIO_PUBLIC_ENDPOINT is not configured")
	}
	u, err := m.publicClient.Presign(ctx, http.MethodPut, bucket, key, expiry, url.Values{
		"partNumber": []string{strconv.Itoa(partNumber)},
		"uploadId":   []string{uploadID},
	})
	if err != nil {
		return "", fmt.Errorf("presign part %d of %s/%s: %w", partNumber, bucket, key, err)
	}
	return u.String(), nil
}

// PublicEndpointConfigured reports whether PAYLASH_MINIO_PUBLIC_ENDPOINT was
// set — i.e. whether the browser can be handed presigned URLs that talk to
// MinIO directly, for both large resumable uploads and direct downloads.
func (m *MinioClient) PublicEndpointConfigured() bool {
	return m.publicClient != nil
}

// UploadedPart describes one part MinIO has already received for an
// in-progress multipart upload.
type UploadedPart struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// ListUploadedParts reports which parts of an in-progress multipart upload
// have already arrived — used to resume an interrupted upload without
// re-sending parts MinIO already has.
func (m *MinioClient) ListUploadedParts(ctx context.Context, bucket, key, uploadID string) ([]UploadedPart, error) {
	var parts []UploadedPart
	marker := 0
	for {
		result, err := m.core.ListObjectParts(ctx, bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, fmt.Errorf("list parts of %s/%s: %w", bucket, key, err)
		}
		for _, p := range result.ObjectParts {
			parts = append(parts, UploadedPart{PartNumber: p.PartNumber, ETag: p.ETag, Size: p.Size})
		}
		if !result.IsTruncated {
			break
		}
		marker = result.NextPartNumberMarker
	}
	return parts, nil
}

// CompletedPart is one part's number and ETag, as returned by MinIO after a
// successful PUT — required to finalize the multipart upload.
type CompletedPart struct {
	PartNumber int
	ETag       string
}

// CompleteMultipartUpload finalizes an upload once every part has arrived,
// and returns the final object's real size.
func (m *MinioClient) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) (int64, error) {
	completeParts := make([]minio.CompletePart, len(parts))
	for i, p := range parts {
		completeParts[i] = minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	if _, err := m.core.CompleteMultipartUpload(ctx, bucket, key, uploadID, completeParts, minio.PutObjectOptions{}); err != nil {
		return 0, fmt.Errorf("complete multipart upload %s/%s: %w", bucket, key, err)
	}
	// CompleteMultipartUpload's own response doesn't reliably report Size,
	// so stat the finished object for its authoritative size instead.
	info, err := m.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("stat completed upload %s/%s: %w", bucket, key, err)
	}
	return info.Size, nil
}

// AbortMultipartUpload cancels an in-progress multipart upload and releases
// whatever parts MinIO had already buffered for it — otherwise they'd
// silently occupy storage forever.
func (m *MinioClient) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	if err := m.core.AbortMultipartUpload(ctx, bucket, key, uploadID); err != nil {
		return fmt.Errorf("abort multipart upload %s/%s: %w", bucket, key, err)
	}
	return nil
}

func PersonalBucket(userID int) string {
	return fmt.Sprintf("personal-%d", userID)
}

func ProjectBucket(projectID int) string {
	return fmt.Sprintf("project-%d", projectID)
}
