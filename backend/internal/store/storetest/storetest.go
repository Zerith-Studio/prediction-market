// Package storetest provides an isolated scratch Postgres database per test
// run: CREATE DATABASE pm_test_<rand> against the DATABASE_URL server, bootstrap
// the schema, and DROP it on cleanup. Safe to point at a shared dev server
// (Neon) — tests never touch the main database's tables.
package storetest

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Zerith-Studio/prediction-market/backend/internal/store"
)

// URL returns the configured DATABASE_URL, falling back to a .env file found by
// walking up from the working directory. Skips the test when unset.
func URL(t testing.TB) string {
	t.Helper()
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		if u := fromEnvFile(filepath.Join(dir, ".env")); u != "" {
			return u
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Skip("DATABASE_URL not set and no .env found — skipping Postgres-backed test")
	return ""
}

func fromEnvFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if v, ok := strings.CutPrefix(line, "DATABASE_URL="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// Open creates a scratch database, opens a bootstrapped Store on it, and
// registers cleanup that drops the database.
func Open(t testing.TB) *store.Store {
	t.Helper()
	adminURL := URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("storetest: rand: %v", err)
	}
	dbName := "pm_test_" + hex.EncodeToString(suffix[:])

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("storetest: connect admin: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		admin.Close(ctx)
		t.Fatalf("storetest: create database: %v", err)
	}
	admin.Close(ctx)

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("storetest: parse url: %v", err)
	}
	u.Path = "/" + dbName
	s, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatalf("storetest: open scratch store: %v", err)
	}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatalf("storetest: bootstrap: %v", err)
	}

	t.Cleanup(func() {
		s.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		admin, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			t.Logf("storetest: cleanup connect: %v", err)
			return
		}
		defer admin.Close(ctx)
		if _, err := admin.Exec(ctx, "DROP DATABASE "+dbName+" WITH (FORCE)"); err != nil {
			t.Logf("storetest: drop database %s: %v", dbName, err)
		}
	})
	return s
}
