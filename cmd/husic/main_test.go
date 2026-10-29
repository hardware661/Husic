package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSecretFilesNameTheFlagThatFailed(t *testing.T) {
	dir := t.TempDir()
	if _, err := readSecret("-db-file", filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "-db-file") {
		t.Fatalf("missing file: %v", err)
	}
	empty := filepath.Join(dir, "empty")
	_ = os.WriteFile(empty, []byte("\n"), 0o600)
	if _, err := readSecret("-gateway-key-file", empty); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("empty file: %v", err)
	}
	full := filepath.Join(dir, "full")
	_ = os.WriteFile(full, []byte(" postgres://x \n"), 0o600)
	if v, err := readSecret("-db-file", full); err != nil || v != "postgres://x" {
		t.Fatalf("value: %q %v", v, err)
	}
}

func TestAccountStateIsCheckedAtStartup(t *testing.T) {
	if err := privateDir("-account-state", ""); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("empty path: %v", err)
	}
	dir := filepath.Join(t.TempDir(), "accounts")
	if err := privateDir("-account-state", dir); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("created: %v %v", info, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("write check left a file behind")
	}
}

func TestOptionsReadHusicEnvironment(t *testing.T) {
	t.Setenv("HUSIC_ACCOUNT_STATE", "/data/accounts")
	if v := option("account-state", "", ""); *v != "/data/accounts" {
		t.Fatalf("got %q", *v)
	}
}
