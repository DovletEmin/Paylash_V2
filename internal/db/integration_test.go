package db

// Integration tests that exercise real SQL against a real Postgres instance
// -- everything else in this package's test suite (and internal/api's) uses
// stub interfaces specifically to avoid this dependency, but a handful of
// things (round-tripping a session through actual INSERT/SELECT, confirming
// Migrate() is genuinely safe to run twice) are only meaningful against a
// real database.
//
// Skipped entirely unless TEST_DATABASE_URL is set, so `go test ./...` stays
// green with no environment set up (this sandbox included -- Postgres here
// only has a Docker-internal address, unreachable from the host Go
// toolchain). Point it at a disposable database, e.g.:
//
//	TEST_DATABASE_URL="postgres://paylash:paylash_secret@localhost:5432/paylash?sslmode=disable" go test ./internal/db/... -run Integration -v
//
// Verified during development by temporarily publishing the compose
// postgres service's port and running exactly that.

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"paylash/internal/models"
)

func connectTestDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration test")
	}
	database, err := Connect(dsn)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := database.Migrate(); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// createTestUser inserts a throwaway user with a unique username and
// registers its own cleanup, so integration tests can run repeatedly
// against a shared, persistent database without colliding on UNIQUE
// constraints or leaving rows behind.
func createTestUser(t *testing.T, database *DB, role string) *models.User {
	t.Helper()
	uname := fmt.Sprintf("itest_%d_%d", time.Now().UnixNano(), rand.Intn(1_000_000)) // #nosec G404 -- test-only uniqueness, not security-sensitive
	user, err := database.CreateUser(&models.RegisterRequest{Username: uname, FullName: "Integration Test"}, "x", role, models.DefaultQuotaBytes, false)
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

func TestIntegrationMigrateIsIdempotent(t *testing.T) {
	database := connectTestDB(t)
	// connectTestDB already ran Migrate() once (fresh connection); running it
	// again here is the actual assertion -- every statement must tolerate
	// being re-applied to an already-migrated schema, which is this
	// project's entire migration strategy (see the doc comment on Migrate).
	if err := database.Migrate(); err != nil {
		t.Fatalf("second Migrate() call failed, migrations are not idempotent: %v", err)
	}
}

func TestIntegrationSessionLifecycle(t *testing.T) {
	database := connectTestDB(t)
	user := createTestUser(t, database, "user")

	session, err := database.CreateSession(user.ID, models.DefaultSessionTTL)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if session.ImpersonatorID != nil {
		t.Errorf("a normal session must not have ImpersonatorID set, got %v", *session.ImpersonatorID)
	}

	got, err := database.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != user.ID {
		t.Errorf("GetSession UserID = %d, want %d", got.UserID, user.ID)
	}
	wantExpiry := time.Now().Add(7 * 24 * time.Hour)
	if got.ExpiresAt.Before(wantExpiry.Add(-time.Minute)) || got.ExpiresAt.After(wantExpiry.Add(time.Minute)) {
		t.Errorf("ExpiresAt = %v, want ~7 days from now (%v)", got.ExpiresAt, wantExpiry)
	}

	if err := database.DeleteSession(session.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := database.GetSession(session.ID); err == nil {
		t.Errorf("GetSession after DeleteSession should fail, got no error")
	}
}

// TestIntegrationImpersonationSession is the DB-layer half of the admin
// "log in as" feature (internal/api/admin.go's Impersonate) -- a session
// with ImpersonatorID set must resolve back to the target user for GetUser
// (every normal permission check), while still carrying the real admin's id
// for the audit log to find. Short-lived (2h, not the normal 7 days) by
// design: an impersonation session left open is a much bigger blast radius
// than a forgotten login.
func TestIntegrationImpersonationSession(t *testing.T) {
	database := connectTestDB(t)
	admin := createTestUser(t, database, "admin")
	target := createTestUser(t, database, "user")

	session, err := database.CreateImpersonationSession(target.ID, admin.ID)
	if err != nil {
		t.Fatalf("CreateImpersonationSession: %v", err)
	}
	if session.UserID != target.ID {
		t.Errorf("session.UserID = %d, want target id %d", session.UserID, target.ID)
	}
	if session.ImpersonatorID == nil || *session.ImpersonatorID != admin.ID {
		t.Fatalf("session.ImpersonatorID = %v, want %d", session.ImpersonatorID, admin.ID)
	}

	got, err := database.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserID != target.ID {
		t.Errorf("GetSession().UserID = %d, want target id %d (permission checks must run as the target)", got.UserID, target.ID)
	}
	if got.ImpersonatorID == nil || *got.ImpersonatorID != admin.ID {
		t.Errorf("GetSession().ImpersonatorID = %v, want %d (audit log must find the real actor)", got.ImpersonatorID, admin.ID)
	}

	maxExpiry := time.Now().Add(2*time.Hour + time.Minute)
	if got.ExpiresAt.After(maxExpiry) {
		t.Errorf("impersonation session ExpiresAt = %v, should be within ~2 hours, not the normal 7 days", got.ExpiresAt)
	}
}

// TestIntegrationBlockedUsersAreSymmetric is the DB-layer guarantee
// SendMessage relies on (internal/api/chat.go) to reject a direct message
// in EITHER blocking direction, not just blocker-to-blocked.
func TestIntegrationBlockedUsersAreSymmetric(t *testing.T) {
	database := connectTestDB(t)
	alice := createTestUser(t, database, "user")
	bob := createTestUser(t, database, "user")

	if err := database.BlockUser(alice.ID, bob.ID); err != nil {
		t.Fatalf("BlockUser: %v", err)
	}
	t.Cleanup(func() { database.UnblockUser(alice.ID, bob.ID) })

	blocked, err := database.IsEitherBlocked(alice.ID, bob.ID)
	if err != nil {
		t.Fatalf("IsEitherBlocked(alice, bob): %v", err)
	}
	if !blocked {
		t.Errorf("IsEitherBlocked(alice, bob) = false, want true (alice blocked bob)")
	}

	blockedReverse, err := database.IsEitherBlocked(bob.ID, alice.ID)
	if err != nil {
		t.Fatalf("IsEitherBlocked(bob, alice): %v", err)
	}
	if !blockedReverse {
		t.Errorf("IsEitherBlocked(bob, alice) = false, want true (block must be checked symmetrically)")
	}
}
