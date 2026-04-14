# gitcnf

`gitcnf` is a small Go CLI that stores Git config values in a local SQLite database, then lets you inspect, export, apply, and group them into named profiles.

## Features

- Store Git config entries in SQLite
- Support `global` and `local` scopes
- List and retrieve saved values
- Remove saved values
- Export saved values as `git config` commands
- Apply saved values directly to Git
- Save named profiles from current config entries
- Reuse a named profile later with `profile use`

## Requirements

- Go 1.22+
- Git

## Install

```bash
go mod tidy
go build -o gitcnf .
```

If you are using the local Go install created in this environment:

```bash
PATH=/home/node/.local/go/bin:$PATH go mod tidy
PATH=/home/node/.local/go/bin:$PATH go build -o gitcnf .
```

## Usage

```bash
./gitcnf init
./gitcnf set --scope global user.name "Anii"
./gitcnf set --scope global user.email "anii@example.com"
./gitcnf list
./gitcnf get --scope global user.name
./gitcnf profile save work
./gitcnf profile list
./gitcnf profile show work
./gitcnf profile use work
./gitcnf profile use --apply work
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

## Profile workflow

Save your current active entries as a named profile:

```bash
./gitcnf set --scope global user.name "Anil Chauhan"
./gitcnf set --scope global user.email "aniicrite@gmail.com"
./gitcnf profile save personal
```

Later, load that profile back into the active store:

```bash
./gitcnf profile use personal
```

Or load and apply it to Git immediately:

```bash
./gitcnf profile use --apply personal
```

## Example export

```bash
git config --global user.email "anii@example.com"
git config --global user.name "Anii"
```

## Notes

- `apply --scope local` must be run inside a Git repository.
- `profile use --apply` applies entries by their stored scope.
- This version stores one value per `scope + section.key` inside both active config storage and each named profile.
