<div align="center">

# DeLLM

**Remove the “AI flavor” from LLM-generated Chinese Markdown**

DeLLM combines Markdown-aware local normalization rules with composable LLM
steps to produce natural, stable, well-typeset Chinese documents.

English · [简体中文](README.md)

</div>

---

## Features

- **Three-stage pipeline**: preprocess → LLM rewriting → postprocess
- **Zero configuration**: the default profile uses local rules only
- **Markdown-aware processing**: code, HTML, tables, emphasis markers, and front matter are protected
- **Composable rules**: quotes, indentation, pangu spacing, emphasis spacing, separators, and heading shifts
- **Structured LLM rewriting**: annotated documents, JSON responses, ID/prefix validation, and safe offset-based reassembly
- **Configuration-driven**: profiles, project/user configuration, and per-document front matter overrides
- **Unix-friendly output**: stdin/stdout, `--format json`, unified diff, and atomic in-place writes
- **Cross-platform binaries**: Linux, macOS, Windows, and Termux Android arm64

## Installation

### Install with Go

```bash
go install github.com/RobiNexy/de-llm/cmd@latest
```

### Download a binary

Download a platform archive from [Releases](https://github.com/RobiNexy/de-llm/releases).

Termux / Android arm64:

```bash
curl -LO https://github.com/RobiNexy/de-llm/releases/latest/download/dellm_android_arm64.tar.gz
tar -xzf dellm_android_arm64.tar.gz
mv dellm_android_arm64/dellm "$PREFIX/bin/"
```

### Build from source

```bash
git clone https://github.com/RobiNexy/de-llm.git
cd de-llm
go build -trimpath -o dellm ./cmd
```

## Quick start

```bash
dellm article.md                         # write to stdout
dellm article.md -i                      # atomic in-place update
dellm article.md -o clean.md             # write to a file
cat article.md | dellm -                 # read from stdin
dellm '*.md' -i                          # batch processing
dellm article.md --format json           # machine-readable output
dellm article.md --diff                  # unified diff
dellm article.md --only quotes,pangu     # run selected rules only
dellm article.md --skip-llm              # skip all LLM steps
dellm article.md -p full --dry-run       # preview steps and token estimates
dellm article.md --shift-headings=-1 -i  # raise all headings by one level
dellm list-rules                         # list local rules
dellm list-prompts                       # list prompt templates
```

Logs go to stderr and never contaminate document output. Use `--quiet` to
hide non-error messages and `--log-level debug|info|warn|error` to select the
log level.

## Built-in rules

| Rule | Function |
|------|----------|
| `quotes` | Convert straight quotes to Chinese curly quotes while protecting apostrophes |
| `remove_separators` | Remove Markdown horizontal rules without touching front matter or Setext headings |
| `shift_headings` | Shift ATX heading levels with clamp/error/skip overflow handling |
| `indent` | Add two ideographic spaces to paragraph starts |
| `pangu` | Add spaces between CJK and Latin letters or digits |
| `emphasis_space` | Add spaces between emphasis markers and adjacent CJK text |

The default profile runs local rules only, so an API key is not required.

## Configuration

Configuration is layered from low to high priority:

```text
built-in defaults → ~/.config/dellm/config.yaml → project dellm.yaml
→ Markdown front matter → CLI flags
```

Minimal LLM configuration:

```yaml
version: 1

profiles:
  full:
    preprocess: [remove_separators]
    llm_steps:
      - name: rewrite headings
        action: rewrite
        target: heading
        filter: { level: [1, 2, 3] }
        prompt: { rewrite: rewrite_heading }
      - name: remove contrasting patterns
        action: rewrite
        target: paragraph
        detect: true
        prompt:
          detect: detect_contrasting
          rewrite: rewrite_contrasting
    postprocess: [quotes, indent, pangu, emphasis_space]

llm:
  providers:
    default:
      type: openai
      base_url: https://api.deepseek.com/v1
      api_key: "${DEEPSEEK_API_KEY}"
      model: deepseek-chat
```

Per-document overrides are supported through front matter:

```markdown
---
dellm:
  profile: quick
  skip: [indent]
---
```

## Custom prompts

Add Markdown templates with YAML front matter under the project `prompts/`
directory to add or override prompts:

```markdown
---
name: my_style
description: My writing style
version: 1
params:
  required: [annotated_doc, target_nodes]
---

Rewrite the following content in the requested style:

{{ .annotated_doc }}

Target nodes:
{{ .target_nodes }}
```

## Architecture

```text
cmd/                  executable entrypoint
internal/command/     thin CLI command layer
pkg/core/             Document, RegionMap, and AnnotatedDoc
pkg/parser/           goldmark parsing and region annotation
pkg/rule/             local rules and registry
pkg/llm/              providers, steps, prompts, and JSON reassembly
pkg/pipeline/         preprocess/LLM/postprocess orchestration
pkg/config/           layered configuration and front matter
pkg/output/           text and JSON formatters
```

The core packages are independent of the CLI and can be tested or reused by
other programs.

## Development and verification

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

Build release archives with:

```bash
./scripts/build-release.sh v0.2.0 dist
```

## License

[MIT](LICENSE)
