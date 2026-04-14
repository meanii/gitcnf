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
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.Exec(`
INSERT INTO config_entries (scope, section, key_name, value, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(scope, section, key_name)
DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at;
`, *scope, section, key, value, now, now)
	if err != nil {
		return fmt.Errorf("save config: %w", err)
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

	var value string
	err = db.QueryRow(`SELECT value FROM config_entries WHERE scope = ? AND section = ? AND key_name = ?`, *scope, section, key).Scan(&value)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("config %s.%s not found for %s scope", section, key, *scope)
		}
		return fmt.Errorf("read config: %w", err)
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

	rows, err := db.Query(`SELECT section, key_name, value FROM config_entries WHERE scope = ? ORDER BY section, key_name`, *scope)
	if err != nil {
		return fmt.Errorf("query configs: %w", err)
	}
	defer rows.Close()

	applied := 0
	for rows.Next() {
		var section, key, value string
		if err := rows.Scan(&section, &key, &value); err != nil {
			return fmt.Errorf("scan config: %w", err)
		}

		gitArgs := []string{"config"}
		if *scope == "global" {
			gitArgs = append(gitArgs, "--global")
		} else if *scope == "local" {
			gitArgs = append(gitArgs, "--local")
		} else {
			return fmt.Errorf("unsupported scope %q", *scope)
		}
		gitArgs = append(gitArgs, section+"."+key, value)

		cmd := exec.Command("git", gitArgs...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("apply %s.%s: %w", section, key, err)
		}
		applied++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate configs: %w", err)
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
  init                         Initialize the SQLite database
  set [--scope s] key value    Save a config value
  get [--scope s] key          Read a saved config value
  list [--scope s]             List saved configs
  remove [--scope s] key       Delete a saved config
  apply [--scope s]            Apply saved configs using git config
  export [--scope s]           Export saved configs as git commands
  help                         Show this help

Examples:
  gitcnf set --scope global user.name "Anii"
  gitcnf set --scope global user.email "me@example.com"
  gitcnf list
  gitcnf apply --scope global
`)
}
