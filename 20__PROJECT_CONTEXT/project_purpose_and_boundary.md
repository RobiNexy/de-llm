# Project purpose and boundary

> Owned by the human. The agent may draft; write only after explicit confirmation. Mark unknowns as "Unconfirmed". The content language follows the project.

## One-line positioning

DeLLM 是一个命令行工具，用于去除 LLM 生成的中文 Markdown 文档中的“AI 味”。它通过 Markdown 感知的本地规则和可编排的 LLM 步骤，把文本整理为自然、稳定、符合中文排版习惯的文档。LLM 改写功能依赖 API Key，必须配置后才能使用；默认 profile 只运行本地规则，可零配置完成基础排版。

## Goals and success criteria

Unconfirmed: the outcomes wanted, and how you will know they are achieved.

## Explicitly out of scope

Unconfirmed. Delete this section if there is nothing notable.

## Key constraints

- People: single developer
- Stack: Go 1.24 CLI, using goldmark for Markdown parsing (detected)
- Deployment target: cross-platform binaries for Linux, macOS, Windows and Termux Android arm64 (detected)
- LLM rewriting requires a configured API Key; the default local-rules profile does not (confirmed)
- Budget and compliance: Unconfirmed
