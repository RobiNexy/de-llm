<div align="center">

# DeLLM

**去除 LLM 生成的中文 Markdown 文档中的 "AI 味"**

基于本地规范化规则与可编排的 LLM 改写步骤，将 LLM 输出转化为
符合中文排版习惯、风格自然的文档。

[English](README.en.md) · 简体中文

</div>

---

## 特性

- **三段式流水线**：预处理（本地规则）→ LLM 改写 → 后处理（本地规则），职责分离
- **零配置可用**：不加任何配置即可完成引号、缩进、盘古之白等基础清理
- **Markdown 感知**：基于 AST 识别文档结构，代码块、表格、front matter 不被误伤
- **缓存友好**：LLM 交互采用"整篇输入 + JSON 结构化输出 + 程序回贴"，最大化 prefix cache 复用
- **配置驱动**：所有行为由 YAML 描述，支持 profile 切换与 front matter 文档级覆盖
- **单二进制**：Go 实现，跨平台分发，支持 amd64 / arm64（含 Termux/Android）

## 安装

### Go 安装

```bash
go install github.com/RobiNexy/de-llm@latest
```

### 下载二进制

从 [Releases](https://github.com/RobiNexy/de-llm/releases) 下载对应平台的压缩包。

**Termux / Android（arm64）**：

```bash
# 在 Termux 中
curl -LO https://github.com/RobiNexy/de-llm/releases/latest/download/dellm_linux_arm64.tar.gz
tar -xzf dellm_linux_arm64.tar.gz && mv dellm_linux_arm64/dellm "$PREFIX/bin/"
```

### 源码构建

```bash
git clone https://github.com/RobiNexy/de-llm.git
cd de-llm && go build -o dellm .
```

## 快速开始

```bash
dellm input.md                          # 输出到 stdout
dellm input.md -i                       # 就地修改
cat input.md | dellm -                  # 从 stdin 读取
dellm *.md -i                           # 批量就地修改
dellm input.md --only quotes,pangu      # 只做引号和盘古之白
dellm input.md -p full --dry-run        # 预览完整流程会做什么
dellm input.md --shift-headings=-1 -i   # 所有标题提升一级
dellm list-rules                        # 列出可用规则
dellm list-prompts                      # 列出可用提示词
```

## 内置本地规则

| 规则 | 功能 |
|------|------|
| `quotes` | 半角引号 → 中文弯引号（配对计数，保护英文撇号） |
| `remove_separators` | 删除章节间的水平分隔线（保护 front matter 与 Setext 标题） |
| `shift_headings` | 标题级别整体提升或下降（修复 LLM 错位的层级） |
| `indent` | 段首缩进两个全角空格 |
| `pangu` | 中英文之间自动插入空格（盘古之白） |
| `emphasis_space` | `**`、`*` 强调标记与中文之间插入空格 |

## 配置

配置查找顺序：`--config` → `./dellm.yaml` → 逐级向上至 git 根 → `~/.config/dellm/config.yaml`。
合并优先级：**CLI flags > front matter > 项目配置 > 用户配置 > 内置默认**。

最小配置示例（启用 LLM 改写，使用 DeepSeek）：

```yaml
version: 1

profiles:
  full:
    preprocess:
      - remove_separators
    llm_steps:
      - name: "重写标题"
        action: rewrite
        target: heading
        detect: false
        filter: { level: [1, 2, 3] }
        prompt: { rewrite: rewrite_heading }
      - name: "去掉转折对比句式"
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

在文档 front matter 中做文档级覆盖：

```markdown
---
dellm:
  profile: quick
  skip: [indent]
---

# 正文
```

## LLM 交互模式

`rewrite` 动作采用**整篇标注 + JSON 结构化输出 + 程序回贴**：

```
原文 → 标注段落号 [P1]/[H1] → 整篇发给 LLM
     → LLM 返回 JSON（id + prefix + 新文本）
     → 程序校验 id 与前缀 → 按位置从后向前回贴
```

该模式使同一 step 内的多轮调用（检测轮 → 改写轮）共享相同的 prompt
前缀，最大化 API 端 prefix cache 的命中率，直接降低费用。

## 自定义提示词

在 `./prompts/` 下放置 Markdown 文件（YAML front matter + text/template 正文），
同名覆盖内置提示词：

```markdown
---
name: my_style
description: 我的自定义风格
version: 1
params:
  required: [annotated_doc, target_nodes]
---

请按以下风格改写以下段落：

{{ .annotated_doc }}

目标段落：
{{ .target_nodes }}
```

## 与设计文档的已知偏差

- **中文标点与 ASCII 之间不插空格**：设计文档 §6.7 的规则表要求中文标点
  （含弯引号）与 ASCII 字母数字之间插入空格，但这会产生 `mmap 。`、
  `“ hello ”` 这类破坏"符合中文排版习惯"目标（§1.1）的输出。实现采用
  排版惯例（pangu.js 行为）：全角标点紧贴前后字符，仅汉字触发插入。
- **配置管理未使用 viper**：profile 的"整体替换"合并语义 viper 不支持，
  仍需手写合并逻辑；改用 `gopkg.in/yaml.v3` 直接反序列化到强类型结构，
  避免 map 往返的类型损伤。

## 开发

```bash
go test ./...        # 全部测试（含 golden file 与集成测试）
go test -race ./...  # 竞态检测
go vet ./...         # 静态检查
```

## 许可证

[MIT](LICENSE)
