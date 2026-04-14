# gitcnf

`gitcnf` is a small Go CLI that stores Git config values in a local SQLite database, then lets you inspect, export, or apply them later.

## Features

- Store Git config entries in SQLite
- Support `global` and `local` scopes
- List and retrieve saved values
- Remove saved values
- Export saved values as `git config` commands
- Apply saved values directly to Git

## Requirements

- Go 1.22+
- Git

## Install

```bash
go mod tidy
go build -o gitcnf .
```

## Usage

```bash
./gitcnf init
./gitcnf set --scope global user.name "Anii"
./gitcnf set --scope global user.email "anii@example.com"
./gitcnf list
./gitcnf get --scope global user.name
./gitcnf export --scope global
./gitcnf apply --scope global
```

By default the SQLite database lives at:

```bash
~/.gitcnf/gitcnf.db
```

You can override it:

```bash
./gitcnf --db ./gitcnf.db list
```

## Example export

```bash
git config --global user.email "anii@example.com"
git config --global user.name "Anii"
```

## Notes

- `apply --scope local` must be run inside a Git repository.
- This version stores one value per `scope + section.key`.
- Because Go is not installed in the current environment yet, this project may need `go mod tidy` and a quick compile/test pass once Go is available.
