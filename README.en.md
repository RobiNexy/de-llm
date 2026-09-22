<div align="center">

# DeLLM

**Remove the "AI flavor" from LLM-generated Chinese Markdown documents**

Transforms LLM output into natural, well-typeset Chinese documents
through local normalization rules and composable LLM rewrite steps.

English · [简体中文](README.md)

</div>

---

## Features

- **Three-stage pipeline**: preprocess (local rules) → LLM rewriting → postprocess (local rules)
- **Zero-config**: basic cleanup (quotes, indentation, pangu spacing) works without any setup
- **Markdown-aware**: AST-based structure recognition; code blocks, tables, and front matter are never touched
- **Cache-friendly**: LLM interaction uses "whole-document input + JSON structured output + programmatic reassembly", maximizing prefix cache hits
- **Configuration-driven**: all behavior described in YAML, with profile switching and per-document front matter overrides
- **Single binary**: Go implementation, cross-platform, amd64 / arm64 (including Termux/Android)

## Installation

### Via Go

```bash
go install github.com/RobiNexy/de-llm@latest
```

### Download binary

Grab a platform archive from [Releases](https://github.com/RobiNexy/de-llm/releases).

**Termux / Android (arm64)**:

```bash
# Inside Termux
curl -LO https://github.com/RobiNexy/de-llm/releases/latest/download/dellm_linux_arm64.tar.gz
tar -xzf dellm_linux_arm64.tar.gz && mv dellm_linux_arm64/dellm "$PREFIX/bin/"
```

### Build from source

```bash
git clone https://github.com/RobiNexy/de-llm.git
cd de-llm && go build -o dellm .
```

## Quick start

```bash
dellm input.md                          # output to stdout
dellm input.md -i                       # in-place
cat input.md | dellm -                  # from stdin
dellm *.md -i                           # batch in-place
dellm input.md --only quotes,pangu      # quotes + pangu only
dellm input.md -p full --dry-run        # preview what the full profile does
dellm input.md --shift-headings=-1 -i   # shift all headings up one level
dellm list-rules                        # list local rules
dellm list-prompts                      # list prompt templates
```

## Built-in local rules

| Rule | Function |
|------|----------|
| `quotes` | Straight quotes → Chinese curly quotes (pairing state machine, protects English apostrophes) |
| `remove_separators` | Remove horizontal rules between sections (protects front matter and Setext headings) |
| `shift_headings` | Shift all heading levels up/down (fixes misaligned heading hierarchy) |
| `indent` | Insert two ideographic spaces at paragraph starts |
| `pangu` | Insert spaces between CJK and Latin/digits (pangu spacing) |
| `emphasis_space` | Insert spaces around `**`/`*` emphasis markers adjacent to CJK |

## Configuration

Lookup order: `--config` → `./dellm.yaml` → upward to git root → `~/.config/dellm/config.yaml`.
Merge priority: **CLI flags > front matter > project config > user config > built-in defaults**.

Minimal example (enable LLM rewriting with DeepSeek):

```yaml
version: 1

profiles:
  full:
    preprocess:
      - remove_separators
    llm_steps:
      - name: "rewrite headings"
        action: rewrite
        target: heading
        detect: false
        filter: { level: [1, 2, 3] }
        prompt: { rewrite: rewrite_heading }
      - name: "remove contrasting patterns"
        action: rewrite
        target: paragraph
        detect: true
        prompt:
          detect: detect_contrasting
          rewrite: rewrite_contrasting
    postprocess:
      - quotes
      - indent
      - pangu
      - emphasis_space

llm:
  providers:
    default:
      type: openai
      base_url: "https://api.deepseek.com/v1"
      api_key: "${DEEPSEEK_API_KEY}"
      model: "deepseek-chat"
```

Per-document overrides via front matter:

```markdown
---
dellm:
  profile: quick
  skip: [indent]
---

# Body
```

## LLM interaction model

The `rewrite` action uses **whole-document annotation + JSON structured output + programmatic reassembly**:

```
source → annotate nodes [P1]/[H1] → send whole annotated doc to LLM
       → LLM returns JSON (id + prefix + new text)
       → program validates id and prefix → replaces back-to-front by offset
```

This lets multiple rounds within one step (detect round → rewrite round) share
the same prompt prefix, maximizing the API-side prefix cache hit rate and
directly reducing cost.

## Custom prompts

Place Markdown files (YAML front matter + text/template body) under `./prompts/`;
same-named files override built-ins:

```markdown
---
name: my_style
description: my custom style
version: 1
params:
  required: [annotated_doc, target_nodes]
---

Rewrite the following paragraphs in this style:

{{ .annotated_doc }}

Target paragraphs:
{{ .target_nodes }}
```

## Known deviations from the design document

- **No space between CJK punctuation and ASCII**: the rule table in §6.7 of the
  design document asks for spaces between CJK punctuation (including curly
  quotes) and ASCII, which produces output like `mmap 。` and `“ hello ”` —
  contradicting the stated goal of idiomatic Chinese typesetting (§1.1). The
  implementation follows typographic convention (pangu.js behavior): full-width
  punctuation stays attached; only ideographs trigger insertion.
- **viper not used for configuration**: viper does not support the
  "whole-profile replacement" merge semantics, which must be hand-written
  anyway. `gopkg.in/yaml.v3` deserializes directly into typed structs,
  avoiding type damage from map round-trips.

## Development

```bash
go test ./...        # all tests (golden files + integration)
go test -race ./...  # race detection
go vet ./...         # static checks
```

## License

[MIT](LICENSE)
