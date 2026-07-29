package storage

import "testing"

func TestPersonalBucket(t *testing.T) {
	if got := PersonalBucket(42); got != "personal-42" {
		t.Errorf("PersonalBucket(42) = %q, want %q", got, "personal-42")
	}
}

func TestProjectBucket(t *testing.T) {
	if got := ProjectBucket(7); got != "project-7" {
		t.Errorf("ProjectBucket(7) = %q, want %q", got, "project-7")
	}
}

// TestThumbnailKeyVarysByVersion is the property the /thumbnail endpoint's
// far-future immutable cache header relies on (see ThumbnailKey's own
// comment): a re-upload/autosave that bumps a file's version MUST produce a
// different key, or a stale cached thumbnail would be served forever.
func TestThumbnailKeyVarysByVersion(t *testing.T) {
	k1 := ThumbnailKey(10, 1)
	k2 := ThumbnailKey(10, 2)
	if k1 == k2 {
		t.Errorf("ThumbnailKey(10,1) and ThumbnailKey(10,2) must differ, both got %q", k1)
	}
	if got, want := ThumbnailKey(10, 1), "10-v1.jpg"; got != want {
		t.Errorf("ThumbnailKey(10,1) = %q, want %q", got, want)
	}
}

func TestThumbnailKeyVarysByFile(t *testing.T) {
	if ThumbnailKey(1, 1) == ThumbnailKey(2, 1) {
		t.Errorf("ThumbnailKey must differ between different file ids")
	}
}
