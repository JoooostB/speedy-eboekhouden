package database

import (
	"context"
	"os"
	"testing"
)

func testDB(t *testing.T) *DB {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set — skipping integration test")
	}

	db, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting to test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestCreateAndGetUser(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	user, err := db.CreateUser(ctx, "test@example.com", "Test User", nil)
	if err != nil {
		t.Fatalf("creating user: %v", err)
	}
	if user.ID == "" {
		t.Fatal("user ID should not be empty")
	}
	if user.Email != "test@example.com" {
		t.Fatalf("email mismatch: %s", user.Email)
	}

	fetched, err := db.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("getting user by ID: %v", err)
	}
	if fetched.Email != user.Email {
		t.Fatal("fetched user email mismatch")
	}

	byEmail, err := db.GetUserByEmail(ctx, "test@example.com")
	if err != nil {
		t.Fatalf("getting user by email: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Fatal("fetched by email ID mismatch")
	}

	// Cleanup
	db.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
}

func TestCreateTeamWithOwner(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	user, _ := db.CreateUser(ctx, "team-owner@example.com", "Owner", nil)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, "DELETE FROM team_members WHERE user_id = $1", user.ID)
		db.Pool.Exec(ctx, "DELETE FROM teams WHERE owner_id = $1", user.ID)
		db.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	team, err := db.CreateTeam(ctx, "Test Team", user.ID)
	if err != nil {
		t.Fatalf("creating team: %v", err)
	}
	if team.Name != "Test Team" {
		t.Fatalf("team name mismatch: %s", team.Name)
	}

	// Owner should be a member
	isMember, err := db.IsMember(ctx, team.ID, user.ID)
	if err != nil {
		t.Fatalf("checking membership: %v", err)
	}
	if !isMember {
		t.Fatal("owner should be a member of the team")
	}

	// List teams
	teams, err := db.GetTeamsByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("listing teams: %v", err)
	}
	if len(teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(teams))
	}

	// List members
	members, err := db.GetMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("listing members: %v", err)
	}
	if len(members) != 1 || members[0].Role != "owner" {
		t.Fatal("expected 1 owner member")
	}
}

func TestSettingsEncryptedKey(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	user, _ := db.CreateUser(ctx, "settings@example.com", "Settings User", nil)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, "DELETE FROM user_settings WHERE user_id = $1", user.ID)
		db.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	// Initially no key
	settings, err := db.GetSettings(ctx, user.ID)
	if err != nil {
		t.Fatalf("getting settings: %v", err)
	}
	if settings.HasAnthropicKey {
		t.Fatal("should not have key initially")
	}

	// Set key
	fakeEncrypted := []byte("encrypted-api-key-data")
	err = db.SetAnthropicKey(ctx, user.ID, fakeEncrypted)
	if err != nil {
		t.Fatalf("setting key: %v", err)
	}

	settings, _ = db.GetSettings(ctx, user.ID)
	if !settings.HasAnthropicKey {
		t.Fatal("should have key after setting")
	}

	// Delete key
	err = db.DeleteAnthropicKey(ctx, user.ID)
	if err != nil {
		t.Fatalf("deleting key: %v", err)
	}

	settings, _ = db.GetSettings(ctx, user.ID)
	if settings.HasAnthropicKey {
		t.Fatal("should not have key after deletion")
	}
}

func TestPasskeyCredentialStorage(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	user, _ := db.CreateUser(ctx, "passkey@example.com", "Passkey User", nil)
	t.Cleanup(func() {
		db.Pool.Exec(ctx, "DELETE FROM passkey_credentials WHERE user_id = $1", user.ID)
		db.Pool.Exec(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})

	cred := &PasskeyCredential{
		ID:              []byte("cred-id-123"),
		UserID:          user.ID,
		PublicKey:       []byte("public-key-data"),
		AttestationType: "none",
		AAGUID:          []byte("aaguid-data"),
		Transport:       []string{"internal"},
		SignCount:       0,
		FriendlyName:    "Test Key",
	}

	err := db.StoreCredential(ctx, cred)
	if err != nil {
		t.Fatalf("storing credential: %v", err)
	}

	creds, err := db.GetCredentialsByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("getting credentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(creds))
	}
	if creds[0].FriendlyName != "Test Key" {
		t.Fatalf("friendly name mismatch: %s", creds[0].FriendlyName)
	}

	// Update sign count
	err = db.UpdateSignCount(ctx, []byte("cred-id-123"), 5)
	if err != nil {
		t.Fatalf("updating sign count: %v", err)
	}

	fetched, err := db.GetCredentialByID(ctx, []byte("cred-id-123"))
	if err != nil {
		t.Fatalf("getting credential by ID: %v", err)
	}
	if fetched.SignCount != 5 {
		t.Fatalf("sign count mismatch: %d", fetched.SignCount)
	}

	// Delete
	err = db.DeleteCredential(ctx, []byte("cred-id-123"), user.ID)
	if err != nil {
		t.Fatalf("deleting credential: %v", err)
	}

	creds, _ = db.GetCredentialsByUserID(ctx, user.ID)
	if len(creds) != 0 {
		t.Fatal("credential should be deleted")
	}
}
