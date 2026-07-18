package db

// FileLocation is a bare bucket/key pair — used by the janitor to sweep
// MinIO version history without pulling in full file rows.
type FileLocation struct {
	Bucket string
	Key    string
}

// ListAllFileLocations returns the bucket/key of every file row, including
// trashed ones (old versions of a trashed-but-not-yet-purged file still
// count against the version retention window).
func (d *DB) ListAllFileLocations() ([]FileLocation, error) {
	rows, err := d.Query(`SELECT DISTINCT minio_bucket, minio_key FROM files`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var locs []FileLocation
	for rows.Next() {
		var l FileLocation
		if err := rows.Scan(&l.Bucket, &l.Key); err != nil {
			return nil, err
		}
		locs = append(locs, l)
	}
	return locs, rows.Err()
}
