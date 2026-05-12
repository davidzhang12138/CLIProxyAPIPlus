package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	spoolDir := filepath.Join(tmpDir, "spool")
	store, err := NewSQLiteStore(context.Background(), SQLiteStoreConfig{
		DBPath:   dbPath,
		SpoolDir: spoolDir,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return store
}

func TestSQLiteStore_SaveAndList(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	metadata := map[string]any{
		"type":  "gemini",
		"email": "test@example.com",
	}
	auth := &cliproxyauth.Auth{
		ID:       "test-token.json",
		Provider: "gemini",
		FileName: "test-token.json",
		Metadata: metadata,
	}

	path, err := store.Save(ctx, auth)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if path == "" {
		t.Fatal("Save returned empty path")
	}

	auths, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(auths))
	}
	if auths[0].Provider != "gemini" {
		t.Errorf("expected provider gemini, got %s", auths[0].Provider)
	}
	if auths[0].ID == "" {
		t.Error("expected non-empty ID")
	}
}

func TestSQLiteStore_Delete(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	auth := &cliproxyauth.Auth{
		ID:       "delete-me.json",
		Provider: "claude",
		FileName: "delete-me.json",
		Metadata: map[string]any{"type": "claude"},
	}
	if _, err := store.Save(ctx, auth); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := store.Delete(ctx, "delete-me.json"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	auths, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(auths) != 0 {
		t.Fatalf("expected 0 auths after delete, got %d", len(auths))
	}
}

func TestSQLiteStore_PersistConfig(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	configContent := []byte("server:\n  port: 8080\n")
	if err := os.WriteFile(store.ConfigPath(), configContent, 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	if err := store.PersistConfig(ctx); err != nil {
		t.Fatalf("PersistConfig: %v", err)
	}

	// Verify the config is stored in the database.
	var content string
	query := "SELECT content FROM " + quoteIdentifier(store.cfg.ConfigTable) + " WHERE id = ?"
	if err := store.db.QueryRowContext(ctx, query, sqliteDefaultConfigKey).Scan(&content); err != nil {
		t.Fatalf("query config: %v", err)
	}
	if content != string(configContent) {
		t.Errorf("expected config %q, got %q", string(configContent), content)
	}
}

func TestSQLiteStore_Bootstrap(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	if err := store.Bootstrap(ctx, ""); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Config file should exist (empty).
	if _, err := os.Stat(store.ConfigPath()); err != nil {
		t.Errorf("config file should exist after bootstrap: %v", err)
	}

	// Auth directory should exist.
	info, err := os.Stat(store.AuthDir())
	if err != nil {
		t.Errorf("auth dir should exist after bootstrap: %v", err)
	} else if !info.IsDir() {
		t.Error("AuthDir should be a directory")
	}
}

func TestSQLiteStore_SaveOverwrite(t *testing.T) {
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	v1 := &cliproxyauth.Auth{
		ID:       "overwrite.json",
		Provider: "gemini",
		FileName: "overwrite.json",
		Metadata: map[string]any{"type": "gemini", "version": "v1"},
	}
	if _, err := store.Save(ctx, v1); err != nil {
		t.Fatalf("Save v1: %v", err)
	}

	v2 := &cliproxyauth.Auth{
		ID:       "overwrite.json",
		Provider: "gemini",
		FileName: "overwrite.json",
		Metadata: map[string]any{"type": "gemini", "version": "v2"},
	}
	if _, err := store.Save(ctx, v2); err != nil {
		t.Fatalf("Save v2: %v", err)
	}

	auths, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(auths) != 1 {
		t.Fatalf("expected 1 auth after overwrite, got %d", len(auths))
	}

	// Verify the metadata is v2.
	raw, err := json.Marshal(auths[0].Metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["version"] != "v2" {
		t.Errorf("expected version v2, got %v", meta["version"])
	}
}
