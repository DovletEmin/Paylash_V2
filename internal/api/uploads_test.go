package api

import "testing"

func TestComputeUploadParts(t *testing.T) {
	tests := []struct {
		name          string
		totalSize     int64
		wantPartSize  int64
		wantPartCount int
	}{
		{name: "tiny file, one part", totalSize: 1024, wantPartSize: uploadPartSize, wantPartCount: 1},
		{name: "exactly one part", totalSize: uploadPartSize, wantPartSize: uploadPartSize, wantPartCount: 1},
		{name: "one byte over one part needs a second", totalSize: uploadPartSize + 1, wantPartSize: uploadPartSize, wantPartCount: 2},
		{name: "150MB needs three 64MB parts", totalSize: 150 << 20, wantPartSize: uploadPartSize, wantPartCount: 3},
		{name: "exactly at the 10000-part ceiling with default part size", totalSize: uploadPartSize * maxUploadParts, wantPartSize: uploadPartSize, wantPartCount: maxUploadParts},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			partSize, partCount := computeUploadParts(tt.totalSize)
			if partSize != tt.wantPartSize {
				t.Errorf("partSize = %d, want %d", partSize, tt.wantPartSize)
			}
			if partCount != tt.wantPartCount {
				t.Errorf("partCount = %d, want %d", partCount, tt.wantPartCount)
			}
			// Invariants that must hold regardless of the exact numbers above.
			if partCount > maxUploadParts {
				t.Errorf("partCount = %d exceeds the S3/MinIO ceiling of %d", partCount, maxUploadParts)
			}
			if int64(partCount-1)*partSize >= tt.totalSize && tt.totalSize > 0 {
				t.Errorf("parts overshoot: (partCount-1)*partSize = %d must be < totalSize = %d", int64(partCount-1)*partSize, tt.totalSize)
			}
			if int64(partCount)*partSize < tt.totalSize {
				t.Errorf("parts undershoot: partCount*partSize = %d must be >= totalSize = %d", int64(partCount)*partSize, tt.totalSize)
			}
		})
	}
}

func TestComputeUploadPartsGrowsPastCeiling(t *testing.T) {
	// One byte past the point where 64MB parts would need more than
	// maxUploadParts parts — the function must grow the part size instead
	// of returning a part count S3/MinIO would reject.
	var totalSize int64 = uploadPartSize*maxUploadParts + 1

	partSize, partCount := computeUploadParts(totalSize)

	if partSize <= uploadPartSize {
		t.Fatalf("expected part size to grow past %d, got %d", uploadPartSize, partSize)
	}
	if partCount > maxUploadParts {
		t.Fatalf("partCount = %d exceeds the S3/MinIO ceiling of %d", partCount, maxUploadParts)
	}
	if int64(partCount)*partSize < totalSize {
		t.Fatalf("parts undershoot: partCount*partSize = %d must be >= totalSize = %d", int64(partCount)*partSize, totalSize)
	}
}
