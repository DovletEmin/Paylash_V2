// Package janitor runs the daily background cleanup sweep: purging trash
// past its retention window (and, in later phases, trimming old file
// versions and aborting stale upload sessions).
package janitor

import (
	"context"
	"log"
	"time"

	"paylash/internal/db"
	"paylash/internal/storage"
)

const (
	trashRetention    = 30 * 24 * time.Hour
	versionRetention  = 90 * 24 * time.Hour
	staleUploadCutoff = 24 * time.Hour
)

// Run performs an immediate cleanup pass and then repeats it once a day.
// It blocks, so callers should invoke it in its own goroutine.
func Run(database *db.DB, minioClient *storage.MinioClient) {
	runOnce(database, minioClient)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		runOnce(database, minioClient)
	}
}

func runOnce(database *db.DB, minioClient *storage.MinioClient) {
	if err := purgeExpiredTrash(database, minioClient); err != nil {
		log.Printf("janitor: purge expired trash: %v", err)
	}
	if err := trimOldVersions(database, minioClient); err != nil {
		log.Printf("janitor: trim old versions: %v", err)
	}
	if err := abortStaleUploads(database, minioClient); err != nil {
		log.Printf("janitor: abort stale uploads: %v", err)
	}
	if err := database.CleanExpiredSessions(); err != nil {
		log.Printf("janitor: clean expired sessions: %v", err)
	}
	if err := database.CleanExpiredTokens(); err != nil {
		log.Printf("janitor: clean expired WOPI tokens: %v", err)
	}
}

// abortStaleUploads cancels multipart uploads nobody has touched (via
// init/part-url/status) in over 24 hours — otherwise the parts MinIO
// buffered for an abandoned upload would occupy storage forever.
func abortStaleUploads(database *db.DB, minioClient *storage.MinioClient) error {
	ctx := context.Background()
	cutoff := time.Now().Add(-staleUploadCutoff)

	sessions, err := database.ListStaleUploadSessions(cutoff)
	if err != nil {
		return err
	}
	aborted := 0
	for _, s := range sessions {
		if err := minioClient.AbortMultipartUpload(ctx, s.Bucket, s.ObjectKey, s.MinioUploadID); err != nil {
			log.Printf("janitor: abort upload %s: %v", s.ID, err)
			continue
		}
		if err := database.DeleteUploadSession(s.ID); err != nil {
			log.Printf("janitor: delete upload session %s: %v", s.ID, err)
			continue
		}
		aborted++
	}
	if aborted > 0 {
		log.Printf("janitor: aborted %d stale upload session(s)", aborted)
	}
	return nil
}

// trimOldVersions removes non-current MinIO object versions past the
// retention window, so bucket versioning doesn't grow storage unbounded —
// especially for large CAD/render files that get overwritten often.
func trimOldVersions(database *db.DB, minioClient *storage.MinioClient) error {
	ctx := context.Background()
	cutoff := time.Now().Add(-versionRetention)

	locations, err := database.ListAllFileLocations()
	if err != nil {
		return err
	}
	trimmed := 0
	for _, loc := range locations {
		versions, err := minioClient.ListVersions(ctx, loc.Bucket, loc.Key)
		if err != nil {
			log.Printf("janitor: list versions %s/%s: %v", loc.Bucket, loc.Key, err)
			continue
		}
		for _, v := range versions {
			if v.IsLatest || v.LastModified.After(cutoff) {
				continue
			}
			if err := minioClient.RemoveVersion(ctx, loc.Bucket, loc.Key, v.VersionID); err != nil {
				log.Printf("janitor: remove version %s of %s/%s: %v", v.VersionID, loc.Bucket, loc.Key, err)
				continue
			}
			trimmed++
		}
	}
	if trimmed > 0 {
		log.Printf("janitor: trimmed %d old file version(s)", trimmed)
	}
	return nil
}

func purgeExpiredTrash(database *db.DB, minioClient *storage.MinioClient) error {
	ctx := context.Background()
	cutoff := time.Now().Add(-trashRetention)

	files, err := database.ListExpiredTrashedFiles(cutoff)
	if err != nil {
		return err
	}
	var fileIDs []int
	for _, f := range files {
		if err := minioClient.Delete(ctx, f.MinioBucket, f.MinioKey); err != nil {
			log.Printf("janitor: delete object %s/%s: %v", f.MinioBucket, f.MinioKey, err)
		}
		fileIDs = append(fileIDs, f.ID)
	}
	if err := database.PurgeFiles(fileIDs); err != nil {
		return err
	}
	if len(fileIDs) > 0 {
		log.Printf("janitor: purged %d expired trashed file(s)", len(fileIDs))
	}

	folderIDs, err := database.ListExpiredTrashedFolderIDs(cutoff)
	if err != nil {
		return err
	}
	if err := database.PurgeFolders(folderIDs); err != nil {
		return err
	}
	if len(folderIDs) > 0 {
		log.Printf("janitor: purged %d expired trashed folder(s)", len(folderIDs))
	}
	return nil
}
