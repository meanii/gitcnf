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

func cmdProfileSave(db *sql.DB, args []string) error {
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
		_, err := db.Exec(`
INSERT INTO profile_entries (profile_name, scope, section, key_name, value, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
`, profileName, entry.Scope, entry.Section, entry.Key, entry.Value, now, now)
		if err != nil {
			return fmt.Errorf("save profile entry %s.%s: %w", entry.Section, entry.Key, err)
		}
	}

	fmt.Printf("saved profile %q with %d entries from %s scope\n", profileName, len(entries), *scope)
	return nil
}

func cmdProfileUse(db *sql.DB, args []string) error {
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

func cmdProfileList(db *sql.DB, args []string) error {
	fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	rows, err := db.Query(`
SELECT profile_name, COUNT(*)
FROM profile_entries
GROUP BY profile_name
ORDER BY profile_name
`)
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

func cmdProfileShow(db *sql.DB, args []string) error {
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

func cmdProfileDelete(db *sql.DB, args []string) error {
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

func upsertConfigEntry(db *sql.DB, scope, section, key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
INSERT INTO config_entries (scope, section, key_name, value, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(scope, section, key_name)
DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;
`, scope, section, key, value, now, now)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return nil
}

func getConfigValue(db *sql.DB, scope, section, key string) (string, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM config_entries WHERE scope = ? AND section = ? AND key_name = ?`, scope, section, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", fmt.Errorf("config %s.%s not found for %s scope", section, key, scope)
		}
		return "", fmt.Errorf("read config: %w", err)
	}
	return value, nil
}

func loadConfigEntries(db *sql.DB, scope string) ([]ConfigEntry, error) {
	rows, err := db.Query(`SELECT id, scope, section, key_name, value, created_at, updated_at FROM config_entries WHERE scope = ? ORDER BY section, key_name`, scope)
	if err != nil {
		return nil, fmt.Errorf("query configs: %w", err)
	}
	defer rows.Close()

	entries := []ConfigEntry{}
	for rows.Next() {
		var entry ConfigEntry
		if err := rows.Scan(&entry.ID, &entry.Scope, &entry.Section, &entry.Key, &entry.Value, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan config: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate configs: %w", err)
	}
	return entries, nil
}

func loadProfileEntries(db *sql.DB, profileName string) ([]ProfileEntry, error) {
	rows, err := db.Query(`
SELECT id, profile_name, scope, section, key_name, value, created_at, updated_at
FROM profile_entries
WHERE profile_name = ?
ORDER BY scope, section, key_name
`, profileName)
	if err != nil {
		return nil, fmt.Errorf("query profile: %w", err)
	}
	defer rows.Close()

	entries := []ProfileEntry{}
	for rows.Next() {
		var entry ProfileEntry
		if err := rows.Scan(&entry.ID, &entry.ProfileName, &entry.Scope, &entry.Section, &entry.Key, &entry.Value, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan profile entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate profile entries: %w", err)
	}
	return entries, nil
}

func filterProfileEntriesByScope(entries []ProfileEntry, scope string) []ConfigEntry {
	filtered := make([]ConfigEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Scope != scope {
			continue
		}
		filtered = append(filtered, ConfigEntry{
			Scope:   entry.Scope,
			Section: entry.Section,
			Key:     entry.Key,
			Value:   entry.Value,
		})
	}
	return filtered
}

func applyEntries(entries []ConfigEntry, scope string) (int, error) {
	applied := 0
	for _, entry := range entries {
		gitArgs := []string{"config"}
		if scope == "global" {
			gitArgs = append(gitArgs, "--global")
		} else if scope == "local" {
			gitArgs = append(gitArgs, "--local")
		} else {
			return 0, fmt.Errorf("unsupported scope %q", scope)
		}
		gitArgs = append(gitArgs, entry.Section+"."+entry.Key, entry.Value)

		cmd := exec.Command("git", gitArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return 0, fmt.Errorf("apply %s.%s: %w", entry.Section, entry.Key, err)
		}
		applied++
	}
	return applied, nil
}

func scopeFlag(scope string) string {
	switch scope {
	case "global":
		return "--global"
	case "local":
		return "--local"
	default:
		return ""
	}
}

func splitConfigKey(input string) (string, string, error) {
	parts := strings.Split(input, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid config key %q, expected section.key", input)
	}
	section := strings.Join(parts[:len(parts)-1], ".")
	key := parts[len(parts)-1]
	return section, key, nil
}

func printUsage() {
	fmt.Println(`gitcnf stores git config entries in SQLite and can apply them later.

Usage:
  gitcnf [--db path] <command> [options]

Commands:
  init                             Initialize the SQLite database
  set [--scope s] key value        Save a config value
  get [--scope s] key              Read a saved config value
  list [--scope s]                 List saved configs
  remove [--scope s] key           Delete a saved config
  apply [--scope s]                Apply saved configs using git config
  export [--scope s]               Export saved configs as git commands
  profile save [--scope s] name    Save current scope entries as a named profile
  profile use [--apply] name       Load a named profile into active configs
  profile list                     List saved profiles
  profile show name                Show entries in a saved profile
  profile delete name              Delete a saved profile
  help                             Show this help

Examples:
  gitcnf set --scope global user.name "Anii"
  gitcnf set --scope global user.email "me@example.com"
  gitcnf profile save work
  gitcnf profile use --apply work
  gitcnf list
`)
}
