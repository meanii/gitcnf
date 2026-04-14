# gitcnf

`gitcnf` is a Go CLI for managing multiple Git identities, SSH key aliases, repo bindings, and plain Git config values.

This project is aimed at the real problem of using multiple GitHub, Bitbucket, or other Git accounts on the same machine, especially when everything points at the same host like `github.com`.

## What it helps with

- store multiple Git identities like personal/work/client
- store SSH host aliases that point to the same real host with different keys
- bind a repo to an identity and SSH alias
- apply a binding to update repo-local Git config and remote URLs
- still keep low-level config/profile tools for simpler workflows

## Features

- plain config storage in SQLite
- profile save/use for grouped config values
- identity management
- SSH alias management
- repo binding management
- SSH config rendering
- remote URL rewriting for SSH aliases
- Go test coverage for core workflows
- GitHub Actions CI and tagged release builds for Linux amd64 and arm64

## Install

```bash
go mod tidy
go build -o gitcnf .
```

If Go is installed locally in this environment:

```bash
PATH=/home/node/.local/go/bin:$PATH go mod tidy
PATH=/home/node/.local/go/bin:$PATH go build -o gitcnf .
```

## Core idea

Instead of trying to make one `github.com` host magically use many keys, create SSH aliases such as:

- `github-personal`
- `github-work`
- `bitbucket-client1`

Then bind repositories to those aliases and identities.

Example remote after binding:

```bash
git@github-work:company/repo.git
```

## Usage

### 1. Add identities

```bash
./gitcnf identity add personal --git-name "Anil Chauhan" --git-email "aniicrite@gmail.com"
./gitcnf identity add work --git-name "Anil Chauhan" --git-email "anil@company.com"
./gitcnf identity list
```

### 2. Add SSH aliases

```bash
./gitcnf ssh add github-personal --host github.com --user git --key ~/.ssh/id_ed25519_github_personal
./gitcnf ssh add github-work --host github.com --user git --key ~/.ssh/id_ed25519_github_work
./gitcnf ssh render
```

Example rendered SSH config:

```ssh
Host github-work
  HostName github.com
  User git
  IdentityFile /home/node/.ssh/id_ed25519_github_work
  IdentitiesOnly yes
```

### 3. Bind a repo

```bash
./gitcnf bind repo ~/code/company-api --identity work --ssh-host github-work
./gitcnf bind list
./gitcnf bind show ~/code/company-api
```

### 4. Apply a binding

```bash
./gitcnf bind apply ~/code/company-api --write-ssh-config
```

That will:
- set repo-local `user.name`
- set repo-local `user.email`
- rewrite the Git remote to use the chosen SSH alias
- optionally append the SSH host block to `~/.ssh/config`

## Example workflow

```bash
./gitcnf identity add work --git-name "Anil Chauhan" --git-email "anil@company.com"
./gitcnf ssh add github-work --host github.com --user git --key ~/.ssh/id_work
./gitcnf bind repo ~/projects/internal-tool --identity work --ssh-host github-work
./gitcnf bind apply ~/projects/internal-tool --write-ssh-config
```

If origin started as:

```bash
git@github.com:company/internal-tool.git
```

it becomes:

```bash
git@github-work:company/internal-tool.git
```

## Legacy config/profile commands

These still exist:

```bash
./gitcnf set --scope global user.name "Anii"
./gitcnf profile save personal
./gitcnf profile use personal
```

## CI and release automation

GitHub Actions included:

- `.github/workflows/ci.yml`
  - runs tests on pushes and pull requests
  - builds the project on Go 1.25 and 1.26

- `.github/workflows/release.yml`
  - runs on tags like `v0.1.0`
  - builds release tarballs for:
    - Linux amd64
    - Linux arm64
  - uploads them to the GitHub release automatically

To cut a release:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## Notes

- `bind apply` expects the repo remote to already be an SSH remote
- current remote rewriting supports common `git@host:org/repo.git` and `ssh://git@host/org/repo.git` formats
- `bind apply --write-ssh-config` appends a host block if it does not already exist
- release builds use `CGO_ENABLED=0`, so if the SQLite driver later requires CGO for your chosen implementation, release packaging may need adjustment
- data is stored in SQLite by default at `~/.gitcnf/gitcnf.db`
