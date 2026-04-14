package main

import (
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type ConfigEntry struct {
	ID        int64
	Scope     string
	Section   string
	Key       string
	Value     string
	CreatedAt string
	UpdatedAt string
}

type ProfileEntry struct {
	ID          int64
	ProfileName string
	Scope       string
	Section     string
	Key         string
	Value       string
	CreatedAt   string
	UpdatedAt   string
}

type Identity struct {
	Name      string
	GitName   string
	GitEmail  string
	CreatedAt string
	UpdatedAt string
}

type SSHHost struct {
	Alias          string
	HostName       string
	UserName       string
	IdentityFile   string
	IdentitiesOnly bool
	CreatedAt      string
	UpdatedAt      string
}

type RepoBinding struct {
	RepoPath    string
	Identity    string
	SSHHost     string
	RemoteName  string
	CreatedAt   string
	UpdatedAt   string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	dbPath, remaining, err := extractGlobalFlags(args)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		printUsage()
		return nil
	}

	db, err := openDB(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	switch remaining[0] {
	case "init":
		return cmdInit(dbPath)
	case "set":
		return cmdSet(db, remaining[1:])
	case "get":
		return cmdGet(db, remaining[1:])
	case "list":
		return cmdList(db, remaining[1:])
	case "remove", "rm":
		return cmdRemove(db, remaining[1:])
	case "apply":
		return cmdApply(db, remaining[1:])
	case "export":
		return cmdExport(db, remaining[1:])
	case "profile":
		return cmdProfile(db, remaining[1:])
	case "identity":
		return cmdIdentity(db, remaining[1:])
	case "ssh":
		return cmdSSH(db, remaining[1:])
	case "bind":
		return cmdBind(db, remaining[1:])
	case "help":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", remaining[0])
	}
}

func extractGlobalFlags(args []string) (string, []string, error) {
	dbPath := defaultDBPath()
	remaining := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--db":
			if i+1 >= len(args) {
				return "", nil, errors.New("--db requires a value")
			}
			dbPath = args[i+1]
			i++
		case strings.HasPrefix(arg, "--db="):
			dbPath = strings.TrimPrefix(arg, "--db=")
		default:
			remaining = append(remaining, arg)
		}
	}

	return dbPath, remaining, nil
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gitcnf.db"
	}
	return filepath.Join(home, ".gitcnf", "gitcnf.db")
}

func openDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if err := initSchema(db); err != nil {
		return nil, err
	}

	return db, nil
}

func initSchema(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS config_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    scope TEXT NOT NULL,
    section TEXT NOT NULL,
    key_name TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(scope, section, key_name)
);
CREATE TABLE IF NOT EXISTS profile_entries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    profile_name TEXT NOT NULL,
    scope TEXT NOT NULL,
    section TEXT NOT NULL,
    key_name TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(profile_name, scope, section, key_name)
);
CREATE TABLE IF NOT EXISTS identities (
    name TEXT PRIMARY KEY,
    git_name TEXT NOT NULL,
    git_email TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS ssh_hosts (
    alias TEXT PRIMARY KEY,
    host_name TEXT NOT NULL,
    user_name TEXT NOT NULL,
    identity_file TEXT NOT NULL,
    identities_only INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS repo_bindings (
    repo_path TEXT PRIMARY KEY,
    identity_name TEXT NOT NULL,
    ssh_alias TEXT NOT NULL,
    remote_name TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY(identity_name) REFERENCES identities(name),
    FOREIGN KEY(ssh_alias) REFERENCES ssh_hosts(alias)
);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}

func cmdInit(dbPath string) error {
	fmt.Printf("initialized database at %s\n", dbPath)
	return nil
}

func cmdSet(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("set", flag.ContinueOnError)
	scope := fs.String("scope", "global", "git config scope: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: gitcnf set [--scope global|local] <section.key> <value>")
	}

	section, key, err := splitConfigKey(fs.Arg(0))
	if err != nil {
		return err
	}
	value := fs.Arg(1)

	if err := upsertConfigEntry(db, *scope, section, key, value); err != nil {
		return err
	}

	fmt.Printf("saved %s.%s for %s scope\n", section, key, *scope)
	return nil
}

func cmdGet(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	scope := fs.String("scope", "global", "git config scope: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf get [--scope global|local] <section.key>")
	}

	section, key, err := splitConfigKey(fs.Arg(0))
	if err != nil {
		return err
	}

	value, err := getConfigValue(db, *scope, section, key)
	if err != nil {
		return err
	}

	fmt.Println(value)
	return nil
}

func cmdList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	scope := fs.String("scope", "", "optional scope filter: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := `SELECT id, scope, section, key_name, value, created_at, updated_at FROM config_entries`
	params := []any{}
	if *scope != "" {
		query += ` WHERE scope = ?`
		params = append(params, *scope)
	}
	query += ` ORDER BY scope, section, key_name`

	rows, err := db.Query(query, params...)
	if err != nil {
		return fmt.Errorf("list configs: %w", err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		found = true
		var entry ConfigEntry
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Section, &entry.Key, &entry.Value, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return fmt.Errorf("scan config: %w", err)
		}
		fmt.Printf("[%s] %s.%s=%s\n", entry.Scope, entry.Section, entry.Key, entry.Value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate configs: %w", err)
	}
	if !found {
		fmt.Println("no saved configs")
	}
	return nil
}

func cmdRemove(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	scope := fs.String("scope", "global", "git config scope: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf remove [--scope global|local] <section.key>")
	}

	section, key, err := splitConfigKey(fs.Arg(0))
	if err != nil {
		return err
	}

	result, err := db.Exec(`DELETE FROM config_entries WHERE scope = ? AND section = ? AND key_name = ?`, *scope, section, key)
	if err != nil {
		return fmt.Errorf("remove config: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("remove config rows affected: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("config %s.%s not found for %s scope", section, key, *scope)
	}

	fmt.Printf("removed %s.%s from %s scope\n", section, key, *scope)
	return nil
}

func cmdApply(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	scope := fs.String("scope", "global", "git config scope to apply: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}

	entries, err := loadConfigEntries(db, *scope)
	if err != nil {
		return err
	}

	applied, err := applyEntries(entries, *scope)
	if err != nil {
		return err
	}

	fmt.Printf("applied %d config entries to git %s scope\n", applied, *scope)
	return nil
}

func cmdExport(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	scope := fs.String("scope", "", "optional scope filter: local or global")
	if err := fs.Parse(args); err != nil {
		return err
	}

	query := `SELECT scope, section, key_name, value FROM config_entries`
	params := []any{}
	if *scope != "" {
		query += ` WHERE scope = ?`
		params = append(params, *scope)
	}
	query += ` ORDER BY scope, section, key_name`

	rows, err := db.Query(query, params...)
	if err != nil {
		return fmt.Errorf("export configs: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var scope, section, key, value string
		if err := rows.Scan(&scope, &section, &key, &value); err != nil {
			return fmt.Errorf("scan config: %w", err)
		}
		fmt.Printf("git config %s %s.%s %q\n", scopeFlag(scope), section, key, value)
	}

	return rows.Err()
}

func cmdProfile(db *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gitcnf profile <save|use|list|show|delete> [options]")
	}

	switch args[0] {
	case "save":
		return cmdProfileSave(db, args[1:])
	case "use":
		return cmdProfileUse(db, args[1:])
	case "list":
		return cmdProfileList(db, args[1:])
	case "show":
		return cmdProfileShow(db, args[1:])
	case "delete", "remove", "rm":
		return cmdProfileDelete(db, args[1:])
	default:
		return fmt.Errorf("unknown profile command %q", args[0])
	}
}

func cmdIdentity(db *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gitcnf identity <add|list|show|delete> [options]")
	}

	switch args[0] {
	case "add":
		return cmdIdentityAdd(db, args[1:])
	case "list":
		return cmdIdentityList(db, args[1:])
	case "show":
		return cmdIdentityShow(db, args[1:])
	case "delete", "remove", "rm":
		return cmdIdentityDelete(db, args[1:])
	default:
		return fmt.Errorf("unknown identity command %q", args[0])
	}
}

func cmdSSH(db *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gitcnf ssh <add|list|show|delete|render> [options]")
	}

	switch args[0] {
	case "add":
		return cmdSSHAdd(db, args[1:])
	case "list":
		return cmdSSHList(db, args[1:])
	case "show":
		return cmdSSHShow(db, args[1:])
	case "delete", "remove", "rm":
		return cmdSSHDelete(db, args[1:])
	case "render":
		return cmdSSHRender(db, args[1:])
	default:
		return fmt.Errorf("unknown ssh command %q", args[0])
	}
}

func cmdBind(db *sql.DB, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: gitcnf bind <repo|list|show|delete|apply> [options]")
	}

	switch args[0] {
	case "repo":
		return cmdBindRepo(db, args[1:])
	case "list":
		return cmdBindList(db, args[1:])
	case "show":
		return cmdBindShow(db, args[1:])
	case "delete", "remove", "rm":
		return cmdBindDelete(db, args[1:])
	case "apply":
		return cmdBindApply(db, args[1:])
	default:
		return fmt.Errorf("unknown bind command %q", args[0])
	}
}

func cmdProfileSave(db *sql.DB, args []string) error { return profileSave(db, args) }
func cmdProfileUse(db *sql.DB, args []string) error { return profileUse(db, args) }
func cmdProfileList(db *sql.DB, args []string) error { return profileList(db, args) }
func cmdProfileShow(db *sql.DB, args []string) error { return profileShow(db, args) }
func cmdProfileDelete(db *sql.DB, args []string) error { return profileDelete(db, args) }

func profileSave(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile save", flag.ContinueOnError)
	scope := fs.String("scope", "global", "scope to save into the profile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf profile save [--scope global|local] <name>")
	}
	profileName := fs.Arg(0)
	entries, err := loadConfigEntries(db, *scope)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no saved configs found for %s scope", *scope)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`DELETE FROM profile_entries WHERE profile_name = ?`, profileName); err != nil {
		return fmt.Errorf("clear existing profile: %w", err)
	}
	for _, entry := range entries {
		_, err := db.Exec(`INSERT INTO profile_entries (profile_name, scope, section, key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, profileName, entry.Scope, entry.Section, entry.Key, entry.Value, now, now)
		if err != nil {
			return fmt.Errorf("save profile entry %s.%s: %w", entry.Section, entry.Key, err)
		}
	}
	fmt.Printf("saved profile %q with %d entries from %s scope\n", profileName, len(entries), *scope)
	return nil
}

func profileUse(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile use", flag.ContinueOnError)
	applyToGit := fs.Bool("apply", false, "apply the profile to git after loading it")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf profile use [--apply] <name>")
	}
	profileName := fs.Arg(0)
	entries, err := loadProfileEntries(db, profileName)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("profile %q not found", profileName)
	}
	for _, entry := range entries {
		if err := upsertConfigEntry(db, entry.Scope, entry.Section, entry.Key, entry.Value); err != nil {
			return err
		}
	}
	fmt.Printf("loaded profile %q into active config store (%d entries)\n", profileName, len(entries))
	if *applyToGit {
		appliedByScope := map[string]int{}
		for _, scope := range []string{"global", "local"} {
			scopeEntries := filterProfileEntriesByScope(entries, scope)
			if len(scopeEntries) == 0 {
				continue
			}
			applied, err := applyEntries(scopeEntries, scope)
			if err != nil {
				return err
			}
			appliedByScope[scope] = applied
		}
		for _, scope := range []string{"global", "local"} {
			if count, ok := appliedByScope[scope]; ok {
				fmt.Printf("applied %d profile entries to git %s scope\n", count, scope)
			}
		}
	}
	return nil
}

func profileList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT profile_name, COUNT(*) FROM profile_entries GROUP BY profile_name ORDER BY profile_name`)
	if err != nil {
		return fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return fmt.Errorf("scan profile: %w", err)
		}
		fmt.Printf("%s (%d entries)\n", name, count)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate profiles: %w", err)
	}
	if !found {
		fmt.Println("no saved profiles")
	}
	return nil
}

func profileShow(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile show", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf profile show <name>")
	}
	entries, err := loadProfileEntries(db, fs.Arg(0))
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("profile %q not found", fs.Arg(0))
	}
	for _, entry := range entries {
		fmt.Printf("[%s] %s.%s=%s\n", entry.Scope, entry.Section, entry.Key, entry.Value)
	}
	return nil
}

func profileDelete(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile delete", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: gitcnf profile delete <name>")
	}
	result, err := db.Exec(`DELETE FROM profile_entries WHERE profile_name = ?`, fs.Arg(0))
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete profile rows affected: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("profile %q not found", fs.Arg(0))
	}
	fmt.Printf("deleted profile %q\n", fs.Arg(0))
	return nil
}

func cmdIdentityAdd(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("identity add", flag.ContinueOnError)
	gitName := fs.String("git-name", "", "git user.name")
	gitEmail := fs.String("git-email", "", "git user.email")
	nameArg, filtered := parseLeadingNameAndFlags(args)
	if nameArg == "" {
		return errors.New("usage: gitcnf identity add <name> --git-name \"Name\" --git-email \"mail@example.com\"")
	}
	if err := fs.Parse(filtered); err != nil {
		return err
	}
	if *gitName == "" || *gitEmail == "" {
		return errors.New("usage: gitcnf identity add <name> --git-name \"Name\" --git-email \"mail@example.com\"")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
INSERT INTO identities (name, git_name, git_email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET git_name = excluded.git_name, git_email = excluded.git_email, updated_at = excluded.updated_at
`, nameArg, *gitName, *gitEmail, now, now)
	if err != nil {
		return fmt.Errorf("save identity: %w", err)
	}
	fmt.Printf("saved identity %q\n", nameArg)
	return nil
}

func cmdIdentityList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("identity list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	rows, err := db.Query(`SELECT name, git_name, git_email FROM identities ORDER BY name`)
	if err != nil { return fmt.Errorf("list identities: %w", err) }
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var i Identity
		if err := rows.Scan(&i.Name, &i.GitName, &i.GitEmail); err != nil { return fmt.Errorf("scan identity: %w", err) }
		fmt.Printf("%s -> %s <%s>\n", i.Name, i.GitName, i.GitEmail)
	}
	if !found { fmt.Println("no saved identities") }
	return rows.Err()
}

func cmdIdentityShow(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("identity show", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf identity show <name>") }
	var i Identity
	err := db.QueryRow(`SELECT name, git_name, git_email, created_at, updated_at FROM identities WHERE name = ?`, fs.Arg(0)).Scan(&i.Name, &i.GitName, &i.GitEmail, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { return fmt.Errorf("identity %q not found", fs.Arg(0)) }
		return fmt.Errorf("show identity: %w", err)
	}
	fmt.Printf("name: %s\ngit name: %s\ngit email: %s\n", i.Name, i.GitName, i.GitEmail)
	return nil
}

func cmdIdentityDelete(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("identity delete", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf identity delete <name>") }
	result, err := db.Exec(`DELETE FROM identities WHERE name = ?`, fs.Arg(0))
	if err != nil { return fmt.Errorf("delete identity: %w", err) }
	count, _ := result.RowsAffected()
	if count == 0 { return fmt.Errorf("identity %q not found", fs.Arg(0)) }
	fmt.Printf("deleted identity %q\n", fs.Arg(0))
	return nil
}

func cmdSSHAdd(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ssh add", flag.ContinueOnError)
	host := fs.String("host", "github.com", "real SSH hostname")
	user := fs.String("user", "git", "SSH user")
	key := fs.String("key", "", "path to SSH private key")
	identitiesOnly := fs.Bool("identities-only", true, "set IdentitiesOnly yes")
	aliasArg, filtered := parseLeadingNameAndFlags(args)
	if aliasArg == "" {
		return errors.New("usage: gitcnf ssh add <alias> --host github.com --user git --key ~/.ssh/id_key")
	}
	if err := fs.Parse(filtered); err != nil { return err }
	if *key == "" {
		return errors.New("usage: gitcnf ssh add <alias> --host github.com --user git --key ~/.ssh/id_key")
	}
	resolvedKey, err := expandPath(*key)
	if err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
INSERT INTO ssh_hosts (alias, host_name, user_name, identity_file, identities_only, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(alias) DO UPDATE SET host_name = excluded.host_name, user_name = excluded.user_name, identity_file = excluded.identity_file, identities_only = excluded.identities_only, updated_at = excluded.updated_at
`, aliasArg, *host, *user, resolvedKey, boolToInt(*identitiesOnly), now, now)
	if err != nil { return fmt.Errorf("save ssh host: %w", err) }
	fmt.Printf("saved ssh host %q\n", aliasArg)
	return nil
}

func cmdSSHList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ssh list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	rows, err := db.Query(`SELECT alias, host_name, user_name, identity_file FROM ssh_hosts ORDER BY alias`)
	if err != nil { return fmt.Errorf("list ssh hosts: %w", err) }
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var h SSHHost
		if err := rows.Scan(&h.Alias, &h.HostName, &h.UserName, &h.IdentityFile); err != nil { return fmt.Errorf("scan ssh host: %w", err) }
		fmt.Printf("%s -> %s (%s, key %s)\n", h.Alias, h.HostName, h.UserName, h.IdentityFile)
	}
	if !found { fmt.Println("no saved ssh hosts") }
	return rows.Err()
}

func cmdSSHShow(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ssh show", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf ssh show <alias>") }
	h, err := getSSHHost(db, fs.Arg(0))
	if err != nil { return err }
	fmt.Printf("alias: %s\nhost: %s\nuser: %s\nidentity file: %s\nidentities only: %t\n", h.Alias, h.HostName, h.UserName, h.IdentityFile, h.IdentitiesOnly)
	return nil
}

func cmdSSHDelete(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ssh delete", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf ssh delete <alias>") }
	result, err := db.Exec(`DELETE FROM ssh_hosts WHERE alias = ?`, fs.Arg(0))
	if err != nil { return fmt.Errorf("delete ssh host: %w", err) }
	count, _ := result.RowsAffected()
	if count == 0 { return fmt.Errorf("ssh host %q not found", fs.Arg(0)) }
	fmt.Printf("deleted ssh host %q\n", fs.Arg(0))
	return nil
}

func cmdSSHRender(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("ssh render", flag.ContinueOnError)
	alias := fs.String("alias", "", "render only one alias")
	if err := fs.Parse(args); err != nil { return err }
	query := `SELECT alias, host_name, user_name, identity_file, identities_only FROM ssh_hosts`
	params := []any{}
	if *alias != "" {
		query += ` WHERE alias = ?`
		params = append(params, *alias)
	}
	query += ` ORDER BY alias`
	rows, err := db.Query(query, params...)
	if err != nil { return fmt.Errorf("render ssh config: %w", err) }
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var h SSHHost
		var identitiesOnly int
		if err := rows.Scan(&h.Alias, &h.HostName, &h.UserName, &h.IdentityFile, &identitiesOnly); err != nil { return fmt.Errorf("scan ssh host: %w", err) }
		h.IdentitiesOnly = identitiesOnly == 1
		fmt.Print(renderSSHHost(h))
	}
	if !found { fmt.Println("no saved ssh hosts") }
	return rows.Err()
}

func cmdBindRepo(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bind repo", flag.ContinueOnError)
	identity := fs.String("identity", "", "identity name")
	sshHost := fs.String("ssh-host", "", "ssh alias")
	remote := fs.String("remote", "origin", "git remote name")
	pathArg, filtered := parseLeadingNameAndFlags(args)
	if pathArg == "" {
		return errors.New("usage: gitcnf bind repo <path> --identity <name> --ssh-host <alias> [--remote origin]")
	}
	if err := fs.Parse(filtered); err != nil { return err }
	if *identity == "" || *sshHost == "" {
		return errors.New("usage: gitcnf bind repo <path> --identity <name> --ssh-host <alias> [--remote origin]")
	}
	repoPath, err := filepath.Abs(pathArg)
	if err != nil { return fmt.Errorf("resolve repo path: %w", err) }
	if _, err := getIdentity(db, *identity); err != nil { return err }
	if _, err := getSSHHost(db, *sshHost); err != nil { return err }
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.Exec(`
INSERT INTO repo_bindings (repo_path, identity_name, ssh_alias, remote_name, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(repo_path) DO UPDATE SET identity_name = excluded.identity_name, ssh_alias = excluded.ssh_alias, remote_name = excluded.remote_name, updated_at = excluded.updated_at
`, repoPath, *identity, *sshHost, *remote, now, now)
	if err != nil { return fmt.Errorf("save repo binding: %w", err) }
	fmt.Printf("bound %s to identity %q via ssh host %q\n", repoPath, *identity, *sshHost)
	return nil
}

func cmdBindList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bind list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	rows, err := db.Query(`SELECT repo_path, identity_name, ssh_alias, remote_name FROM repo_bindings ORDER BY repo_path`)
	if err != nil { return fmt.Errorf("list bindings: %w", err) }
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var b RepoBinding
		if err := rows.Scan(&b.RepoPath, &b.Identity, &b.SSHHost, &b.RemoteName); err != nil { return fmt.Errorf("scan binding: %w", err) }
		fmt.Printf("%s -> identity=%s ssh-host=%s remote=%s\n", b.RepoPath, b.Identity, b.SSHHost, b.RemoteName)
	}
	if !found { fmt.Println("no repo bindings") }
	return rows.Err()
}

func cmdBindShow(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bind show", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf bind show <path>") }
	binding, err := getBinding(db, fs.Arg(0))
	if err != nil { return err }
	fmt.Printf("repo: %s\nidentity: %s\nssh host: %s\nremote: %s\n", binding.RepoPath, binding.Identity, binding.SSHHost, binding.RemoteName)
	return nil
}

func cmdBindDelete(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bind delete", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil { return err }
	if fs.NArg() != 1 { return errors.New("usage: gitcnf bind delete <path>") }
	repoPath, err := filepath.Abs(fs.Arg(0))
	if err != nil { return fmt.Errorf("resolve repo path: %w", err) }
	result, err := db.Exec(`DELETE FROM repo_bindings WHERE repo_path = ?`, repoPath)
	if err != nil { return fmt.Errorf("delete binding: %w", err) }
	count, _ := result.RowsAffected()
	if count == 0 { return fmt.Errorf("binding for %q not found", repoPath) }
	fmt.Printf("deleted binding for %s\n", repoPath)
	return nil
}

func cmdBindApply(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("bind apply", flag.ContinueOnError)
	writeSSH := fs.Bool("write-ssh-config", false, "append ssh host entry to ~/.ssh/config if missing")
	pathArg, filtered := parseLeadingNameAndFlags(args)
	if pathArg == "" { return errors.New("usage: gitcnf bind apply <path> [--write-ssh-config]") }
	if err := fs.Parse(filtered); err != nil { return err }
	repoPath, err := filepath.Abs(pathArg)
	if err != nil { return fmt.Errorf("resolve repo path: %w", err) }
	binding, err := getBinding(db, repoPath)
	if err != nil { return err }
	identity, err := getIdentity(db, binding.Identity)
	if err != nil { return err }
	sshHost, err := getSSHHost(db, binding.SSHHost)
	if err != nil { return err }
	if err := setGitConfig(repoPath, false, "user.name", identity.GitName); err != nil { return err }
	if err := setGitConfig(repoPath, false, "user.email", identity.GitEmail); err != nil { return err }
	remoteURL, changed, err := rewriteRemoteURL(repoPath, binding.RemoteName, sshHost.Alias)
	if err != nil { return err }
	if *writeSSH {
		if err := ensureSSHConfigEntry(sshHost); err != nil { return err }
		fmt.Printf("ensured ssh config entry for %s\n", sshHost.Alias)
	}
	fmt.Printf("applied binding to %s\n", repoPath)
	fmt.Printf("set git identity to %s <%s>\n", identity.GitName, identity.GitEmail)
	if changed {
		fmt.Printf("updated remote %s to %s\n", binding.RemoteName, remoteURL)
	} else {
		fmt.Printf("remote %s already uses %s\n", binding.RemoteName, remoteURL)
	}
	return nil
}

func upsertConfigEntry(db *sql.DB, scope, section, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`INSERT INTO config_entries (scope, section, key_name, value, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(scope, section, key_name) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, scope, section, key, value, now, now)
	if err != nil { return fmt.Errorf("save config: %w", err) }
	return nil
}

func getConfigValue(db *sql.DB, scope, section, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM config_entries WHERE scope = ? AND section = ? AND key_name = ?`, scope, section, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { return "", fmt.Errorf("config %s.%s not found for %s scope", section, key, scope) }
		return "", fmt.Errorf("read config: %w", err)
	}
	return value, nil
}

func loadConfigEntries(db *sql.DB, scope string) ([]ConfigEntry, error) {
	rows, err := db.Query(`SELECT id, scope, section, key_name, value, created_at, updated_at FROM config_entries WHERE scope = ? ORDER BY section, key_name`, scope)
	if err != nil { return nil, fmt.Errorf("query configs: %w", err) }
	defer rows.Close()
	entries := []ConfigEntry{}
	for rows.Next() {
		var entry ConfigEntry
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Section, &entry.Key, &entry.Value, &entry.CreatedAt, &entry.UpdatedAt); err != nil { return nil, fmt.Errorf("scan config: %w", err) }
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate configs: %w", err) }
	return entries, nil
}

func loadProfileEntries(db *sql.DB, profileName string) ([]ProfileEntry, error) {
	rows, err := db.Query(`SELECT id, profile_name, scope, section, key_name, value, created_at, updated_at FROM profile_entries WHERE profile_name = ? ORDER BY scope, section, key_name`, profileName)
	if err != nil { return nil, fmt.Errorf("query profile: %w", err) }
	defer rows.Close()
	entries := []ProfileEntry{}
	for rows.Next() {
		var entry ProfileEntry
		if err := rows.Scan(&entry.ID, &entry.ProfileName, &entry.Scope, &entry.Section, &entry.Key, &entry.Value, &entry.CreatedAt, &entry.UpdatedAt); err != nil { return nil, fmt.Errorf("scan profile entry: %w", err) }
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("iterate profile entries: %w", err) }
	return entries, nil
}

func filterProfileEntriesByScope(entries []ProfileEntry, scope string) []ConfigEntry {
	filtered := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Scope != scope { continue }
		filtered = append(filtered, ConfigEntry{Scope: entry.Scope, Section: entry.Section, Key: entry.Key, Value: entry.Value})
	}
	return filtered
}

func applyEntries(entries []ConfigEntry, scope string) (int, error) {
	applied := 0
	for _, entry := range entries {
		gitArgs := []string{"config"}
		if scope == "global" { gitArgs = append(gitArgs, "--global") } else if scope == "local" { gitArgs = append(gitArgs, "--local") } else { return 0, fmt.Errorf("unsupported scope %q", scope) }
		gitArgs = append(gitArgs, entry.Section+"."+entry.Key, entry.Value)
		cmd := exec.Command("git", gitArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil { return 0, fmt.Errorf("apply %s.%s: %w", entry.Section, entry.Key, err) }
		applied++
	}
	return applied, nil
}

func getIdentity(db *sql.DB, name string) (Identity, error) {
	var i Identity
	err := db.QueryRow(`SELECT name, git_name, git_email, created_at, updated_at FROM identities WHERE name = ?`, name).Scan(&i.Name, &i.GitName, &i.GitEmail, &i.CreatedAt, &i.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { return Identity{}, fmt.Errorf("identity %q not found", name) }
		return Identity{}, fmt.Errorf("get identity: %w", err)
	}
	return i, nil
}

func getSSHHost(db *sql.DB, alias string) (SSHHost, error) {
	var h SSHHost
	var identitiesOnly int
	err := db.QueryRow(`SELECT alias, host_name, user_name, identity_file, identities_only, created_at, updated_at FROM ssh_hosts WHERE alias = ?`, alias).Scan(&h.Alias, &h.HostName, &h.UserName, &h.IdentityFile, &identitiesOnly, &h.CreatedAt, &h.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { return SSHHost{}, fmt.Errorf("ssh host %q not found", alias) }
		return SSHHost{}, fmt.Errorf("get ssh host: %w", err)
	}
	h.IdentitiesOnly = identitiesOnly == 1
	return h, nil
}

func getBinding(db *sql.DB, path string) (RepoBinding, error) {
	repoPath, err := filepath.Abs(path)
	if err != nil { return RepoBinding{}, fmt.Errorf("resolve repo path: %w", err) }
	var b RepoBinding
	err = db.QueryRow(`SELECT repo_path, identity_name, ssh_alias, remote_name, created_at, updated_at FROM repo_bindings WHERE repo_path = ?`, repoPath).Scan(&b.RepoPath, &b.Identity, &b.SSHHost, &b.RemoteName, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) { return RepoBinding{}, fmt.Errorf("binding for %q not found", repoPath) }
		return RepoBinding{}, fmt.Errorf("get binding: %w", err)
	}
	return b, nil
}

func setGitConfig(repoPath string, global bool, key, value string) error {
	args := []string{"-C", repoPath, "config"}
	if global { args = append(args, "--global") }
	args = append(args, key, value)
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil { return fmt.Errorf("set git config %s: %w", key, err) }
	return nil
}

func rewriteRemoteURL(repoPath, remoteName, sshAlias string) (string, bool, error) {
	getCmd := exec.Command("git", "-C", repoPath, "remote", "get-url", remoteName)
	output, err := getCmd.Output()
	if err != nil { return "", false, fmt.Errorf("get remote url: %w", err) }
	current := strings.TrimSpace(string(output))
	rewritten, err := replaceRemoteHost(current, sshAlias)
	if err != nil { return "", false, err }
	if rewritten == current { return rewritten, false, nil }
	setCmd := exec.Command("git", "-C", repoPath, "remote", "set-url", remoteName, rewritten)
	setCmd.Stdout = os.Stdout
	setCmd.Stderr = os.Stderr
	if err := setCmd.Run(); err != nil { return "", false, fmt.Errorf("set remote url: %w", err) }
	return rewritten, true, nil
}

func replaceRemoteHost(remoteURL, alias string) (string, error) {
	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.SplitN(strings.TrimPrefix(remoteURL, "git@"), ":", 2)
		if len(parts) != 2 { return "", fmt.Errorf("unsupported ssh remote format %q", remoteURL) }
		return "git@" + alias + ":" + parts[1], nil
	}
	if strings.HasPrefix(remoteURL, "ssh://") {
		prefix := "ssh://git@"
		if !strings.HasPrefix(remoteURL, prefix) { return "", fmt.Errorf("unsupported ssh remote format %q", remoteURL) }
		rest := strings.TrimPrefix(remoteURL, prefix)
		idx := strings.Index(rest, "/")
		if idx == -1 { return "", fmt.Errorf("unsupported ssh remote format %q", remoteURL) }
		return prefix + alias + rest[idx:], nil
	}
	return "", fmt.Errorf("remote url %q is not an ssh remote", remoteURL)
}

func ensureSSHConfigEntry(host SSHHost) error {
	home, err := os.UserHomeDir()
	if err != nil { return fmt.Errorf("resolve home dir: %w", err) }
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil { return fmt.Errorf("create ssh dir: %w", err) }
	configPath := filepath.Join(sshDir, "config")
	existing, _ := os.ReadFile(configPath)
	block := renderSSHHost(host)
	if strings.Contains(string(existing), "Host "+host.Alias+"\n") {
		return nil
	}
	f, err := os.OpenFile(configPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil { return fmt.Errorf("open ssh config: %w", err) }
	defer f.Close()
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		if _, err := f.WriteString("\n"); err != nil { return fmt.Errorf("write ssh config separator: %w", err) }
	}
	if _, err := f.WriteString(block); err != nil { return fmt.Errorf("append ssh config: %w", err) }
	return nil
}

func renderSSHHost(host SSHHost) string {
	identitiesOnly := "no"
	if host.IdentitiesOnly { identitiesOnly = "yes" }
	return fmt.Sprintf("Host %s\n  HostName %s\n  User %s\n  IdentityFile %s\n  IdentitiesOnly %s\n\n", host.Alias, host.HostName, host.UserName, host.IdentityFile, identitiesOnly)
}

func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil { return "", fmt.Errorf("resolve home dir: %w", err) }
		if path == "~" { return home, nil }
		path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return filepath.Abs(path)
}

func boolToInt(v bool) int {
	if v { return 1 }
	return 0
}

func parseLeadingNameAndFlags(args []string) (string, []string) {
	if len(args) == 0 {
		return "", nil
	}
	if strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

func scopeFlag(scope string) string {
	switch scope {
	case "global": return "--global"
	case "local": return "--local"
	default:
		return ""
	}
}

func splitConfigKey(input string) (string, string, error) {
	parts := strings.Split(input, ".")
	if len(parts) < 2 { return "", "", fmt.Errorf("invalid config key %q, expected section.key", input) }
	section := strings.Join(parts[:len(parts)-1], ".")
	key := parts[len(parts)-1]
	return section, key, nil
}

func printUsage() {
	fmt.Print(`gitcnf manages multiple Git identities, SSH aliases, repo bindings, and plain git config values.

Usage:
  gitcnf [--db path] <command> [options]

Core config commands:
  init
  set [--scope s] key value
  get [--scope s] key
  list [--scope s]
  remove [--scope s] key
  apply [--scope s]
  export [--scope s]

Profile commands:
  profile save [--scope s] name
  profile use [--apply] name
  profile list
  profile show name
  profile delete name

Identity commands:
  identity add <name> --git-name "Name" --git-email "mail@example.com"
  identity list
  identity show <name>
  identity delete <name>

SSH host commands:
  ssh add <alias> --host github.com --user git --key ~/.ssh/id_key
  ssh list
  ssh show <alias>
  ssh delete <alias>
  ssh render [--alias name]

Binding commands:
  bind repo <path> --identity <name> --ssh-host <alias> [--remote origin]
  bind list
  bind show <path>
  bind delete <path>
  bind apply <path> [--write-ssh-config]

Examples:
  gitcnf identity add work --git-name "Anil Chauhan" --git-email "work@example.com"
  gitcnf ssh add github-work --host github.com --user git --key ~/.ssh/id_work
  gitcnf bind repo ~/code/company-api --identity work --ssh-host github-work
  gitcnf bind apply ~/code/company-api --write-ssh-config
`)
}
