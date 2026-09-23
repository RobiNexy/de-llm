<div align="center">

# DeLLM

**去除 LLM 生成的中文 Markdown 文档中的“AI 味”**

通过 Markdown 感知的本地规则和可编排的 LLM 步骤，
把生成式文本整理为自然、稳定、符合中文排版习惯的文档。

[English](README.en.md) · 简体中文

</div>

---

## 特性

- **三段式流水线**：预处理 → LLM 改写 → 后处理
- **零配置可用**：默认只启用本地规则，不需要 API key
- **Markdown 感知**：代码块、HTML、表格、强调标记和 front matter 分区处理
- **可组合规则**：支持 `quotes`、`indent`、`pangu`、`emphasis_space`、`remove_separators`、`shift_headings`
- **结构化 LLM 改写**：整篇标注、JSON 输出、ID 与 prefix 校验、按偏移安全回贴
- **配置驱动**：支持 profile、项目配置、用户配置和文档 front matter 覆盖
- **机器友好**：支持 stdin/stdout、`--format json`、unified diff 和原子就地写入
- **跨平台**：Linux、macOS、Windows，以及 Termux Android arm64

## 安装

### Go 安装

```bash
go install github.com/RobiNexy/de-llm/cmd@latest
```

### 下载二进制

从 [Releases](https://github.com/RobiNexy/de-llm/releases) 下载对应平台的压缩包。

Termux / Android arm64：

```bash
curl -LO https://github.com/RobiNexy/de-llm/releases/latest/download/dellm_android_arm64.tar.gz
tar -xzf dellm_android_arm64.tar.gz
mv dellm_android_arm64/dellm "$PREFIX/bin/"
```

### 从源码构建

```bash
git clone https://github.com/RobiNexy/de-llm.git
cd de-llm
go build -trimpath -o dellm ./cmd
```

## 快速开始

```bash
dellm article.md                         # 输出到 stdout
dellm article.md -i                      # 原子就地修改
dellm article.md -o clean.md             # 写入指定文件
cat article.md | dellm -                 # 从 stdin 读取
dellm '*.md' -i                          # 批量处理
dellm article.md --format json           # 机器可解析输出
dellm article.md --diff                  # 输出 unified diff
dellm article.md --only quotes,pangu     # 只运行指定规则
dellm article.md --skip-llm              # 跳过全部 LLM 步骤
dellm article.md -p full --dry-run       # 预览处理流程和 token 估算
dellm article.md --shift-headings=-1 -i  # 标题整体提升一级
dellm list-rules                         # 列出本地规则
dellm list-prompts                       # 列出提示词
```

日志写入 stderr，不污染文档输出。使用 `--quiet` 静默非错误信息，使用
`--log-level debug|info|warn|error` 控制日志级别。

## 默认规则

| 规则 | 作用 |
|------|------|
| `quotes` | 半角引号转换为中文弯引号，保护英文撇号 |
| `remove_separators` | 删除 Markdown 水平分隔线，保护 front matter 和 Setext 标题 |
| `shift_headings` | 调整 ATX 标题层级，支持越界 clamp/error/skip |
| `indent` | 在正文段落首行插入两个全角空格 |
| `pangu` | 在中文与英文、数字之间插入空格 |
| `emphasis_space` | 在强调标记与中文相邻时补空格 |

默认 profile 只执行本地规则，因此不配置 LLM 也可以直接使用。

## 配置

配置优先级从低到高为：

```text
内置默认 → ~/.config/dellm/config.yaml → 项目 dellm.yaml
→ Markdown front matter → CLI flags
```

最小 LLM 配置示例：

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

文档可以通过 front matter 覆盖 profile、规则选择和规则参数：

```markdown
---
dellm:
  profile: quick
  skip: [indent]
---
```

## 自定义提示词

在项目的 `prompts/` 目录放置带 YAML front matter 的 Markdown 模板即可覆盖或新增提示词：

```markdown
---
name: my_style
description: 我的写作风格
version: 1
params:
  required: [annotated_doc, target_nodes]
---

请按指定风格改写以下内容：

{{ .annotated_doc }}

目标节点：
{{ .target_nodes }}
```

## 架构

```text
cmd/                  可执行程序入口
internal/command/     CLI 命令薄壳
pkg/core/             Document、RegionMap、AnnotatedDoc
pkg/parser/           goldmark Markdown 解析与区域标注
pkg/rule/             本地规则和规则注册表
pkg/llm/              Provider、Step、Prompt 和 JSON 回贴
pkg/pipeline/         预处理、LLM、后处理编排
pkg/config/           配置层叠与 front matter
pkg/output/           text/json 输出格式化
```

核心包不依赖 CLI，可以独立测试和复用。

## 开发与验证

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./...
```

发布构建：

```bash
./scripts/build-release.sh v0.2.0 dist
```

## 许可证

[MIT](LICENSE)
