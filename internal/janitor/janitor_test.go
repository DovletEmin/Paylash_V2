package janitor

// Integration test for purgeExpiredTrash, the highest-risk sweep the
// janitor runs (the only one that permanently destroys data). Unlike
// abortStaleUploads/trimOldVersions/purgeOrphanedChatAttachments it's the
// one where a bug is unrecoverable, so it's the one worth the cost of a real
// DB + MinIO round trip; the others are exercised only indirectly through
// their DB-layer query methods.
//
// Skipped entirely unless TEST_DATABASE_URL and TEST_MINIO_ENDPOINT are both
// set, e.g. (after publishing the compose postgres service's port, which
// already exposes 9000 for MinIO by default):
//
//	TEST_DATABASE_URL="postgres://paylash:paylash_secret@localhost:5432/paylash?sslmode=disable" \
//	TEST_MINIO_ENDPOINT=localhost:9000 TEST_MINIO_ACCESS_KEY=paylash TEST_MINIO_SECRET_KEY=paylash_secret \
//	go test ./internal/janitor/... -v

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"paylash/internal/db"
	"paylash/internal/models"
	"paylash/internal/storage"
)

func connectTestDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	database, err := db.Connect(dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func connectTestMinio(t *testing.T) *storage.MinioClient {
	t.Helper()
	endpoint := os.Getenv("TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("TEST_MINIO_ENDPOINT not set, skipping integration test")
	}
	client, err := storage.NewMinioClient(endpoint, os.Getenv("TEST_MINIO_ACCESS_KEY"), os.Getenv("TEST_MINIO_SECRET_KEY"), false, "")
	if err != nil {
		t.Fatalf("connect to test minio: %v", err)
	}
	return client
}

func createTestUser(t *testing.T, database *db.DB) *models.User {
	t.Helper()
	uname := fmt.Sprintf("itest_%d_%d", time.Now().UnixNano(), rand.Intn(1_000_000)) // #nosec G404 -- test-only uniqueness, not security-sensitive
	user, err := database.CreateUser(&models.RegisterRequest{Username: uname, FullName: "Integration Test"}, "x", "user", models.DefaultQuotaBytes, false)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		if _, err := database.Exec(`DELETE FROM users WHERE id = $1`, user.ID); err != nil {
			t.Logf("cleanup: delete test user %d: %v", user.ID, err)
		}
	})
	return user
}

// TestIntegrationPurgeExpiredTrashDeletesFileAndObject plants a file trashed
// well past the retention window (both the DB row and its real MinIO
// object+thumbnail), runs the exact sweep the daily janitor runs, and checks
// everything is actually gone -- not just that no error was returned.
func TestIntegrationPurgeExpiredTrashDeletesFileAndObject(t *testing.T) {
	database := connectTestDB(t)
	minioClient := connectTestMinio(t)
	ctx := context.Background()

	user := createTestUser(t, database)
	bucket := storage.PersonalBucket(user.ID)
	if err := minioClient.EnsureBucket(ctx, bucket); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	key := fmt.Sprintf("itest-%d.txt", time.Now().UnixNano())
	content := "integration test file content"
	if err := minioClient.Upload(ctx, bucket, key, strings.NewReader(content), int64(len(content)), "text/plain"); err != nil {
		t.Fatalf("upload test object: %v", err)
	}

	f := &models.File{
		Name:        "itest-trash.txt",
		MimeType:    "text/plain",
		SizeBytes:   int64(len(content)),
		MinioBucket: bucket,
		MinioKey:    key,
		OwnerID:     user.ID,
		Scope:       "personal",
	}
	if err := database.CreateFile(f); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	// CreateFile's RETURNING clause doesn't scan back `version` (DB default
	// 1), so f.Version is still Go's zero value here -- re-fetch to match
	// what purgeExpiredTrash will actually read from the row.
	created, err := database.GetFile(f.ID)
	if err != nil || created == nil {
		t.Fatalf("GetFile after create: %v", err)
	}
	f.Version = created.Version

	// Also plant a thumbnail object -- purgeExpiredTrash deletes it
	// alongside the file's own object, and a leaked thumbnail is exactly
	// the kind of bug this test exists to catch.
	thumbKey := storage.ThumbnailKey(f.ID, f.Version)
	if err := minioClient.EnsureBucket(ctx, storage.ThumbnailBucket); err != nil {
		t.Fatalf("EnsureBucket(thumbnails): %v", err)
	}
	if err := minioClient.Upload(ctx, storage.ThumbnailBucket, thumbKey, strings.NewReader("thumb"), 5, "image/jpeg"); err != nil {
		t.Fatalf("upload test thumbnail: %v", err)
	}

	// Back-date deleted_at well past trashRetention (30 days) so this file
	// is picked up as expired regardless of when the test runs.
	if _, err := database.Exec(`UPDATE files SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, f.ID); err != nil {
		t.Fatalf("backdate deleted_at: %v", err)
	}

	if err := purgeExpiredTrash(database, minioClient); err != nil {
		t.Fatalf("purgeExpiredTrash: %v", err)
	}

	remaining, err := database.GetFileIncludingTrash(f.ID)
	if err != nil {
		t.Fatalf("GetFileIncludingTrash: %v", err)
	}
	if remaining != nil {
		t.Errorf("file row %d still exists after purge", f.ID)
	}

	if _, err := minioClient.GetObjectInfo(ctx, bucket, key); err == nil {
		t.Errorf("object %s/%s still exists in MinIO after purge", bucket, key)
	}
	if _, err := minioClient.GetObjectInfo(ctx, storage.ThumbnailBucket, thumbKey); err == nil {
		t.Errorf("thumbnail %s/%s still exists in MinIO after purge", storage.ThumbnailBucket, thumbKey)
	}
}
