# Project conventions

> Record only what is confirmed and not obvious from configuration or code. Empty items are not gates. Items marked "detected" were inferred by the agent and await confirmation.

## Commands

- Install dependencies: `go mod download` (detected)
- Run locally: `go run ./cmd` (detected)
- Tests: `go test ./...` (detected)
- Type check: `go vet ./...` (detected; Go has no separate type-check command)
- Lint / format: `gofmt -l .` (detected; CI fails on non-empty output)
- Build: `go build ./...` (detected)

## Environments

- How to tell local from test/staging from production (env vars, config files, hostnames): local development plus GitHub Actions CI; no staging or production deployment (confirmed)
- Commands or tools that touch non-local environments (treat as high-impact): pushing to GitHub triggers CI; LLM provider calls configured through `dellm.yaml` or `~/.config/dellm/config.yaml` with `${DEEPSEEK_API_KEY}` spend money and leave the machine (confirmed)

## Git

- Branching: trunk-based on `main` (detected)
- Commit style: Conventional Commits prefixes such as `feat:`, `refactor:`, `docs:` (detected)
- Does the agent commit without being asked: yes, small single-purpose commits (confirmed)

## Collaboration

- Default mode (see [[AGENT__collaboration]]): Copilot
- Design documents directory: `design/` unless stated otherwise

## Project-specific exceptions

- Code and test layout: follow the existing layout; note only exceptions the agent must know
- Naming or formatting exceptions: none
- Other long-lived conventions: none
