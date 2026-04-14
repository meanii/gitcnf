package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "gitcnf-test.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return strings.TrimSpace(string(out))
}

func TestConfigSetGetAndProfileFlow(t *testing.T) {
	db := newTestDB(t)

	if err := cmdSet(db, []string{"--scope", "global", "user.name", "Anil Chauhan"}); err != nil {
		t.Fatalf("cmdSet user.name: %v", err)
	}
	if err := cmdSet(db, []string{"--scope", "global", "user.email", "aniicrite@gmail.com"}); err != nil {
		t.Fatalf("cmdSet user.email: %v", err)
	}

	value, err := getConfigValue(db, "global", "user", "name")
	if err != nil {
		t.Fatalf("getConfigValue: %v", err)
	}
	if value != "Anil Chauhan" {
		t.Fatalf("unexpected config value: %q", value)
	}

	if err := cmdProfileSave(db, []string{"--scope", "global", "personal"}); err != nil {
		t.Fatalf("cmdProfileSave: %v", err)
	}

	entries, err := loadProfileEntries(db, "personal")
	if err != nil {
		t.Fatalf("loadProfileEntries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 profile entries, got %d", len(entries))
	}

	if err := cmdRemove(db, []string{"--scope", "global", "user.name"}); err != nil {
		t.Fatalf("cmdRemove: %v", err)
	}
	if err := cmdProfileUse(db, []string{"personal"}); err != nil {
		t.Fatalf("cmdProfileUse: %v", err)
	}

	value, err = getConfigValue(db, "global", "user", "name")
	if err != nil {
		t.Fatalf("getConfigValue after profile use: %v", err)
	}
	if value != "Anil Chauhan" {
		t.Fatalf("unexpected restored value: %q", value)
	}
}

func TestIdentityAndSSHHostStorage(t *testing.T) {
	db := newTestDB(t)

	if err := cmdIdentityAdd(db, []string{"work", "--git-name", "Anil Chauhan", "--git-email", "anil@company.com"}); err != nil {
		t.Fatalf("cmdIdentityAdd: %v", err)
	}
	identity, err := getIdentity(db, "work")
	if err != nil {
		t.Fatalf("getIdentity: %v", err)
	}
	if identity.GitEmail != "anil@company.com" {
		t.Fatalf("unexpected git email: %q", identity.GitEmail)
	}

	keyPath := filepath.Join(t.TempDir(), "id_work")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	if err := cmdSSHAdd(db, []string{"github-work", "--host", "github.com", "--user", "git", "--key", keyPath}); err != nil {
		t.Fatalf("cmdSSHAdd: %v", err)
	}
	sshHost, err := getSSHHost(db, "github-work")
	if err != nil {
		t.Fatalf("getSSHHost: %v", err)
	}
	if sshHost.HostName != "github.com" {
		t.Fatalf("unexpected host name: %q", sshHost.HostName)
	}
	if !strings.Contains(renderSSHHost(sshHost), "Host github-work") {
		t.Fatalf("renderSSHHost did not include alias")
	}
}

func TestBindApplyUpdatesGitConfigAndRemote(t *testing.T) {
	db := newTestDB(t)

	repoDir := t.TempDir()
	runGit(t, repoDir, "init")
	runGit(t, repoDir, "remote", "add", "origin", "git@github.com:example/project.git")

	if err := cmdIdentityAdd(db, []string{"work", "--git-name", "Anil Chauhan", "--git-email", "anil@company.com"}); err != nil {
		t.Fatalf("cmdIdentityAdd: %v", err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_work")
	if err := os.WriteFile(keyPath, []byte("dummy"), 0o600); err != nil {
		t.Fatalf("write key file: %v", err)
	}
	if err := cmdSSHAdd(db, []string{"github-work", "--host", "github.com", "--user", "git", "--key", keyPath}); err != nil {
		t.Fatalf("cmdSSHAdd: %v", err)
	}
	if err := cmdBindRepo(db, []string{repoDir, "--identity", "work", "--ssh-host", "github-work"}); err != nil {
		t.Fatalf("cmdBindRepo: %v", err)
	}
	if err := cmdBindApply(db, []string{repoDir}); err != nil {
		t.Fatalf("cmdBindApply: %v", err)
	}

	if got := runGit(t, repoDir, "config", "user.name"); got != "Anil Chauhan" {
		t.Fatalf("unexpected git user.name: %q", got)
	}
	if got := runGit(t, repoDir, "config", "user.email"); got != "anil@company.com" {
		t.Fatalf("unexpected git user.email: %q", got)
	}
	if got := runGit(t, repoDir, "remote", "get-url", "origin"); got != "git@github-work:example/project.git" {
		t.Fatalf("unexpected remote url: %q", got)
	}
}

func TestReplaceRemoteHost(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		alias  string
		want   string
		wantErr bool
	}{
		{name: "scp style", input: "git@github.com:owner/repo.git", alias: "github-work", want: "git@github-work:owner/repo.git"},
		{name: "ssh url", input: "ssh://git@github.com/owner/repo.git", alias: "github-work", want: "ssh://git@github-work/owner/repo.git"},
		{name: "https unsupported", input: "https://github.com/owner/repo.git", alias: "github-work", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := replaceRemoteHost(tt.input, tt.alias)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("replaceRemoteHost error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("unexpected rewritten url: got %q want %q", got, tt.want)
			}
		})
	}
}
