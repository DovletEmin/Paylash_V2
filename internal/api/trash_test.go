package api

import "testing"

func TestResolveRestoreParent(t *testing.T) {
	id := 42

	tests := []struct {
		name    string
		id      *int
		trashed bool
		want    *int
	}{
		{name: "nil id stays nil (already scope root)", id: nil, trashed: false, want: nil},
		{name: "nil id stays nil even if 'trashed' is somehow true", id: nil, trashed: true, want: nil},
		{name: "live parent is kept", id: &id, trashed: false, want: &id},
		{name: "trashed parent falls back to scope root", id: &id, trashed: true, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveRestoreParent(tt.id, tt.trashed)
			if (got == nil) != (tt.want == nil) {
				t.Fatalf("resolveRestoreParent() = %v, want %v", got, tt.want)
			}
			if got != nil && *got != *tt.want {
				t.Fatalf("resolveRestoreParent() = %d, want %d", *got, *tt.want)
			}
		})
	}
}
