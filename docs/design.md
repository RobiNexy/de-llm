# DeLLM 设计文档 v1.0

## 1. 项目概述

### 1.1 定位

DeLLM 是一个命令行工具，用于去除 LLM 生成的中文 Markdown 文档中的"AI 味"。通过本地规范化规则和可编排的 LLM 改写步骤，将 LLM 输出转化为符合中文排版习惯、风格自然的文档。

### 1.2 设计原则

- **配置驱动**：所有行为由 YAML 配置文件描述，CLI 只做元操作，不为每个规则设 flag
- **三段式流水线**：预处理（本地） → LLM 改写 → 后处理（本地），职责清晰分离
- **Markdown 感知**：基于 AST 识别文档结构，避免误伤代码块、表格等非正文区域
- **缓存友好**：LLM 交互采用"整篇输入 + JSON 结构化输出 + 程序回贴"模式，最大化 prefix cache 利用
- **渐进式使用**：零配置即可做基础清理；加配置可编排 LLM 改写；加自定义提示词可深度定制

### 1.3 技术选型

| 组件 | 选择 | 理由 |
|------|------|------|
| 语言 | Go 1.22+ | 单二进制分发，跨平台 |
| Markdown 解析 | goldmark | AST 结构清晰，活跃维护 |
| CLI 框架 | cobra | Go CLI 标准选择 |
| 配置管理 | viper | 多层级合并、环境变量展开 |
| 内置资源 | go:embed | 内置提示词打包 |
| 模板引擎 | text/template | 标准库 |
| 速率限制 | golang.org/x/time/rate | 标准限流实现 |

---

## 2. 整体架构

### 2.1 处理流程

```
输入文件/stdin
    │
    ▼
┌─────────────────────────┐
│  配置加载 & 合并         │  CLI flags > front matter > 项目配置 > 用户配置 > 内置默认
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│  ① 预处理流水线          │  本地规则：快速、确定性
│  (preprocess)            │  例：shift_headings, remove_separators
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│  ② LLM 处理阶段         │  按顺序执行每个 llm_step
│  (llm_steps)             │  慢、贵、有随机性
│                          │  两种 action:
│                          │    rewrite        → 整篇输入 + JSON 输出 + 回贴
│                          │    full_rewrite   → 整篇输入 + 整篇输出
└────────────┬────────────┘
             │
             ▼
┌─────────────────────────┐
│  ③ 后处理流水线          │  本地规则：快速、确定性
│  (postprocess)           │  例：quotes, indent, pangu, emphasis_space
└────────────┬────────────┘
             │
             ▼
输出到 stdout / 文件 / 就地修改
```

### 2.2 三段式的设计理由

LLM 处理和本地规则有本质区别：

| 维度 | 本地规则 | LLM 步骤 |
|------|----------|----------|
| 速度 | 微秒级 | 秒到分钟级 |
| 成本 | 零 | 按 token 计费 |
| 确定性 | 输入相同输出相同 | 每次可能不同 |
| 失败模式 | 几乎不失败 | 网络、限流、输出格式异常 |

将 LLM 步骤从本地规则中独立出来：
- `--skip-llm` 一键跳过所有 LLM 步骤
- `--dry-run` 对 LLM 输出成本预估
- 失败处理策略可以不同

### 2.3 模块划分

```
dellm/
├── main.go
├── cmd/
│   ├── root.go                   # cobra 根命令
│   ├── list_rules.go             # dellm list-rules
│   └── list_prompts.go           # dellm list-prompts
│
├── config/
│   ├── config.go                 # 配置结构体定义
│   ├── loader.go                 # 多层级加载 & 合并
│   ├── defaults.go               # 内置默认值
│   └── frontmatter.go            # Markdown front matter 配置解析
│
├── markdown/
│   ├── parser.go                 # goldmark 封装
│   ├── region.go                 # RegionMap
│   ├── annotate.go               # LLM 用的文档标注
│   └── render.go                 # AST → Markdown 渲染（用于 shift_headings 等改 AST 的规则）
│
├── pipeline/
│   ├── pipeline.go               # 三段式流水线编排
│   ├── context.go                # 处理上下文
│   └── diff.go                   # diff 生成
│
├── rule/
│   ├── rule.go                   # Rule 接口
│   ├── registry.go               # 规则注册表
│   ├── quotes.go
│   ├── indent.go
│   ├── pangu.go
│   ├── emphasis_space.go
│   ├── remove_separators.go
│   └── shift_headings.go
│
├── llm/
│   ├── step.go                   # LLMStep 执行逻辑
│   ├── action.go                 # rewrite / full_rewrite 实现
│   ├── json_parse.go             # JSON 输出解析与容错
│   ├── apply.go                  # 回贴逻辑（含 prefix 校验）
│   ├── client.go                 # HTTP 客户端（重试、限流）
│   ├── provider.go               # Provider 接口
│   ├── providers/
│   │   ├── openai.go
│   │   ├── anthropic.go
│   │   └── ollama.go
│   └── prompt/
│       ├── prompt.go
│       ├── registry.go
│       ├── loader.go
│       └── builtin/              # go:embed
│           ├── detect_contrasting.md
│           ├── rewrite_contrasting.md
│           ├── rewrite_heading.md
│           ├── elaborate_steps.md
│           ├── compress.md
│           └── polish.md
│
└── testdata/
    ├── quotes/
    ├── indent/
    ├── pangu/
    ├── emphasis_space/
    ├── remove_separators/
    ├── shift_headings/
    └── integration/
```

---

## 3. 配置系统

### 3.1 配置文件查找

按以下顺序查找，找到第一个即停止：

1. CLI `--config` 指定的路径
2. `./dellm.yaml` 或 `./dellm.yml`
3. `./.dellm.yaml` 或 `./.dellm.yml`
4. 从当前目录逐级向上查找，到 git 根或用户主目录停止
5. `~/.config/dellm/config.yaml`

找不到任何配置文件时，使用内置默认配置（仅本地规则）。

### 3.2 配置合并优先级

从低到高（后者覆盖前者）：

```
内置默认值
  ← ~/.config/dellm/config.yaml（用户级）
    ← 项目目录 dellm.yaml（项目级）
      ← Markdown front matter 中的 dellm: 块（文档级）
        ← CLI flags（最高）
```

**合并规则：**
- `profiles` 下的每个 profile 是**整体替换**——profile 是完整行为描述，部分合并会产生不可预测的行为
- `rules` 下的各规则参数是**深度合并**
- `llm.providers` 是**深度合并**

### 3.3 完整配置结构

```yaml
version: 1

# ─── Profiles ─────────────────────────────────────────
profiles:
  # 默认 profile：只做本地规则，零配置即可用
  default:
    preprocess:
      - remove_separators

    llm_steps: []

    postprocess:
      - quotes
      - indent
      - pangu
      - emphasis_space

  # 完整流程示例：包含 LLM 改写
  full:
    preprocess:
      - remove_separators

    llm_steps:
      # 先粗加工：全文细化
      - name: "全文细化"
        action: full_rewrite
        prompt:
          rewrite: elaborate_steps
        params:
          expand_ratio: "1.3"

      # 再精修：标题
      - name: "重写标题"
        action: rewrite
        target: heading
        detect: false
        filter:
          level: [1, 2, 3]
        prompt:
          rewrite: rewrite_heading

      # 再精修：句式
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

  # 快速清理：只做基础排版
  quick:
    preprocess: []
    llm_steps: []
    postprocess:
      - quotes
      - pangu

# ─── 规则参数 ──────────────────────────────────────────
rules:
  quotes:
    style: curly                # 未来可扩展 angle
    single_quotes: true
    apostrophe_protection: true

  remove_separators:
    keep_frontmatter: true

  shift_headings:
    offset: 0                    # 正数下降，负数提升
    scope: []                    # 空数组表示所有级别
    on_overflow: clamp           # clamp | error | skip

  indent:
    char: "\u3000\u3000"         # 两个全角空格
    scope: [paragraph]
    skip_first_paragraph: false

  pangu:
    space_char: " "

  emphasis_space:
    space_char: " "

# ─── LLM 配置 ─────────────────────────────────────────
llm:
  providers:
    default:
      type: openai               # openai | anthropic | ollama
      base_url: "https://api.openai.com/v1"
      api_key: "${DELLM_API_KEY}"
      model: "gpt-4o"
      temperature: 0.3
      max_tokens: 4096
      timeout: 60s
      max_retries: 3

  prompts:
    dirs:
      - "./prompts"                       # 项目本地（最高优先级）
      - "~/.config/dellm/prompts"         # 用户全局

  # 步骤间串行执行（保证 prefix cache 最大化利用）
  # 单个 step 内部（如 detect + rewrite 两轮）也是串行
  concurrency: 1

  rate_limit: "60/minute"

  # 失败策略
  # preserve: 保留原文并 warning
  # fail: 立即终止
  on_failure: preserve

# ─── 全局配置 ──────────────────────────────────────────
global:
  encoding: utf-8
  log_level: info                # debug | info | warn | error
```

### 3.4 Markdown Front Matter 覆盖

单个 Markdown 文件可以在 front matter 中嵌入 DeLLM 配置：

```markdown
---
title: 我的文章
dellm:
  profile: quick
  skip:
    - indent
  rules:
    quotes:
      single_quotes: false
---

# 正文开始
```

支持的字段：
- `profile`：切换到指定 profile
- `skip`：跳过指定规则/步骤（数组）
- `only`：只运行指定规则/步骤（数组，与 skip 互斥）
- `rules`：覆盖规则参数

不支持在 front matter 中定义 `llm_steps` 或 `providers`——这些是项目级关注点。

---

## 4. CLI 接口

### 4.1 命令与 Flag

```
用法:
  dellm [flags] [file...]
  dellm [command]

可用命令:
  list-rules      列出所有可用的本地规则
  list-prompts    列出所有已加载的提示词模板
  help            帮助信息

Flags:
  -c, --config string        配置文件路径
  -p, --profile string       使用指定 profile（默认 "default"）
  -o, --output string        输出文件路径（仅单文件输入时有效）
  -i, --in-place             就地修改
  -d, --diff                 输出 unified diff，不实际写入
      --dry-run              报告将执行的操作，不实际处理
      --only strings         只运行指定规则或步骤（逗号分隔）
      --skip strings         跳过指定规则或步骤（逗号分隔）
      --skip-llm             跳过所有 LLM 步骤
      --shift-headings int   快捷方式：临时启用 shift_headings 并设置 offset
      --stats                输出处理统计
  -v, --verbose              详细日志
  -h, --help                 帮助

示例:
  dellm input.md                          # 输出到 stdout
  dellm input.md -o output.md             # 输出到文件
  dellm input.md -i                       # 就地修改
  cat input.md | dellm -                  # 从 stdin 读取
  dellm *.md -i                           # 批量就地修改
  dellm input.md -p full                  # 使用 full profile
  dellm input.md --only quotes,pangu      # 只做引号和盘古之白
  dellm input.md --skip-llm               # 跳过 LLM 步骤
  dellm input.md --dry-run -p full        # 预览 full profile 会做什么
  dellm input.md --shift-headings=-1 -i   # 快捷：所有标题提升一级
  dellm list-rules                        # 列出可用规则
  dellm list-prompts                      # 列出可用提示词
```

### 4.2 --only / --skip 匹配规则

- 精确匹配规则名或 llm_step 的 name 字段
- 大小写不敏感
- `--only` 和 `--skip` 互斥，同时指定时报错退出
- 未匹配任何规则/步骤时输出 warning 但不报错

### 4.3 --shift-headings 快捷方式

`--shift-headings=N` 等价于：
- 在 preprocess 最前面加一个 shift_headings 规则
- 设置 `rules.shift_headings.offset = N`

如果 profile 中已有 shift_headings，只覆盖其 offset。

### 4.4 就地修改的安全性

`-i` 模式的写入流程：
1. 写入同目录的临时文件（`.dellm.tmp.XXXXXX`）
2. `fsync` 确保数据落盘
3. `os.Rename` 原子替换原文件
4. 任何步骤失败则删除临时文件，原文件不受影响

### 4.5 多文件处理

`dellm *.md -i` 批量处理：
- 每个文件独立处理
- 单个文件失败时输出错误，继续处理下一个
- 全部成功返回 0，任一失败返回 1
- `-o` 在多文件模式下报错退出
- `--stats` 输出每个文件的统计和汇总

### 4.6 list-rules 输出

```
$ dellm list-rules

本地规则:
  quotes              半角引号 → 中文弯引号（配对计数）
  remove_separators   删除章节间的水平分隔线
  shift_headings      标题级别整体提升或下降
  indent              段首缩进（两个全角空格）
  pangu               中英文之间自动插入空格
  emphasis_space      强调标记（**、*）前后自动插入空格
```

### 4.7 list-prompts 输出

```
$ dellm list-prompts

已加载的提示词模板:
  detect_contrasting            检测"不是A而是B"等转折对比句式          [内置]
  rewrite_contrasting           改写转折对比句式段落                    [内置]
  rewrite_heading               重写标题                              [内置]
  elaborate_steps               全文细化，补充跳过的步骤                [内置]
  compress                      压缩文本                              [内置]
  polish                        段落润色                              [内置]
  my_style                      我的自定义风格                        [./prompts/my_style.md]
```

---

## 5. Markdown 解析与区域映射

### 5.1 设计思路

引号替换、盘古之白等操作本质上是字符级的。goldmark 的 AST 修改 API 不是为"改文本内容"设计的——AST 节点通过字节偏移引用原始 source，修改文本会导致偏移失效。

采用**混合方案**：
- 用 goldmark **仅做结构识别**——解析 AST，标记每个字节区间属于什么文档结构
- 各字符级 Rule 基于 RegionMap 对原始文本处理
- 需要修改 AST 结构的规则（如 shift_headings）单独走"AST 修改 + 重新渲染"路径
- LLM 步骤基于 AST 节点做筛选和标注

### 5.2 区域类型

```go
type RegionType int

const (
    RegionText           RegionType = iota  // 正文文本
    RegionCode                               // 代码块 / 行内代码
    RegionHTML                               // HTML 块 / 行内 HTML
    RegionHeading                            // 标题文本
    RegionListItem                           // 列表项文本
    RegionTable                              // 表格内容
    RegionBlockquote                         // 引用块内容
    RegionFrontMatter                        // YAML front matter
    RegionEmphasisMarker                     // ** / * 标记符号本身
)
```

### 5.3 RegionMap 结构

```go
type Region struct {
    Start int
    End   int
    Type  RegionType
    Meta  map[string]any    // 如 heading level, code language, emphasis position
}

type RegionMap struct {
    Regions []Region        // 按 Start 升序，不重叠
    Source  []byte
}
```

### 5.4 RegionMap 构建流程

```
1. 用 goldmark 解析 source 为 AST
2. 深度优先遍历 AST
3. 对每个叶子节点，根据节点类型和祖先链确定 RegionType
4. 记录 (start, end, type, meta) 四元组
5. 未被覆盖的字节区间标记为 RegionText
6. 按 Start 排序，合并相邻同类型区间
```

祖先链的判定：

| AST 节点 | 祖先上下文 | RegionType |
|----------|-----------|------------|
| Text/String | 在 Paragraph 下 | RegionText |
| Text/String | 在 Heading 下 | RegionHeading |
| Text/String | 在 ListItem 下 | RegionListItem |
| Text/String | 在 Blockquote 下 | RegionBlockquote |
| CodeBlock / FencedCodeBlock | 任意 | RegionCode |
| CodeSpan | 任意 | RegionCode |
| RawHTML / HTMLBlock | 任意 | RegionHTML |
| Emphasis 的 delimiter | 任意 | RegionEmphasisMarker |

### 5.5 Rule 执行中的 RegionMap 使用

每个 Rule 执行后可能改变文本长度，导致后续 Rule 的 RegionMap 偏移失效。所以每个 Rule 执行前都重新构建 RegionMap：

```go
for _, rule := range rules {
    regionMap := buildRegionMap(currentSource)
    newSource, err := rule.Transform(regionMap)
    if err != nil { return err }
    currentSource = newSource
}
```

goldmark 解析几十 KB 文档在微秒级，重新构建的成本可以接受。

---

## 6. 本地规则

### 6.1 Rule 接口

```go
type Rule interface {
    Name() string
    Description() string
    Transform(regionMap *RegionMap) ([]byte, error)
}
```

### 6.2 规则注册表

```go
var registry = map[string]func(cfg map[string]any) (Rule, error){
    "quotes":              NewQuotesRule,
    "remove_separators":   NewRemoveSeparatorsRule,
    "shift_headings":      NewShiftHeadingsRule,
    "indent":              NewIndentRule,
    "pangu":               NewPanguRule,
    "emphasis_space":      NewEmphasisSpaceRule,
}
```

### 6.3 quotes — 引号替换

**功能：** 半角双引号 `"` (U+0022) → 中文弯引号 `""` (U+201C / U+201D)；半角单引号 `'` (U+0027) → 中文弯引号 `''` (U+2018 / U+2019)。

**可处理的区域：** RegionText、RegionHeading、RegionListItem、RegionTable、RegionBlockquote。

**跳过的区域：** RegionCode、RegionHTML、RegionFrontMatter、RegionEmphasisMarker。

**配对计数状态机（每段重置）：**

```
维护两个独立状态：
  double_expect_open = true
  single_expect_open = true

对每个可处理区域的每个字符：

  遇到 " (U+0022):
    if double_expect_open: 输出 " (U+201C)
    else:                  输出 " (U+201D)
    翻转 double_expect_open

  遇到 ' (U+0027):
    if apostrophe_protection 已启用:
      if 前一字符是 ASCII 字母 且 后一字符是 ASCII 字母:
        输出原字符（视为英文撇号）
        continue
    if single_expect_open: 输出 ' (U+2018)
    else:                  输出 ' (U+2019)
    翻转 single_expect_open

  其他字符: 原样输出
```

**段边界识别：** 遍历 Region 时，如果当前 Region 和上一个可处理 Region 之间的原始文本包含 `\n\n`，或中间存在类型为 RegionCode/RegionHTML/RegionFrontMatter 的区域，则重置状态。

**段尾未闭合处理：** 记录 warning（含行号），不回退。

### 6.4 remove_separators — 删除分隔线

**功能：** 删除 Markdown 水平分隔线（`---`、`***`、`___`）。

**实现：** 基于 goldmark AST 精确识别，只删除 `ast.ThematicBreak` 节点。这样天然避免误伤：
- YAML front matter 的 `---` 不是 ThematicBreak
- Setext 标题下划线 `===` / `---` 是 Heading 的一部分

**空行合并规则：** 删除分隔线行后，如果前后各有空行（产生 2+ 连续空行），合并为 1 个空行。

### 6.5 shift_headings — 标题级别调整

**功能：** 所有标题的级别整体提升或下降。用于修复 LLM 输出错位的标题层级。

**配置：**

```yaml
rules:
  shift_headings:
    offset: -1                # 正数下降（h1→h2），负数提升（h2→h1）
    scope: [2, 3, 4, 5, 6]    # 只处理这些级别；空数组表示所有
    on_overflow: clamp        # clamp | error | skip
```

**越界处理：**

| on_overflow | 行为 |
|-------------|------|
| clamp | 保持在 1-6 范围（h1 再提升还是 h1，h6 再下降还是 h6） |
| error | 越界时终止流水线并报错 |
| skip | 越界的标题跳过不处理 |

**实现方式：**

这是少数需要修改 AST 结构的规则。流程：
1. goldmark 解析 source
2. 遍历 AST，修改 `ast.Heading` 节点的 `Level` 字段
3. 用自定义 renderer 将 AST 重新渲染为 Markdown

自定义 renderer 的关键：保持原文的 Markdown 语法风格（ATX vs Setext、fenced vs indented code 等）。goldmark 默认 renderer 会规范化输出，可能改变风格。所以需要基于原始 source 做**最小修改**：只重写包含 Heading 的行，其他行原样保留。

```go
func (r *ShiftHeadingsRule) Transform(regionMap *RegionMap) ([]byte, error) {
    doc := parseAST(regionMap.Source)
    changes := []headingChange{}
  
    ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
        if !entering { return ast.WalkContinue, nil }
        heading, ok := n.(*ast.Heading)
        if !ok { return ast.WalkContinue, nil }
      
        oldLevel := heading.Level
        if len(r.scope) > 0 && !contains(r.scope, oldLevel) {
            return ast.WalkContinue, nil
        }
      
        newLevel := clampLevel(oldLevel + r.offset, r.onOverflow)
        if newLevel == 0 {  // skip
            return ast.WalkContinue, nil
        }
        if newLevel == -1 {  // error
            return ast.WalkStop, fmt.Errorf("标题级别越界")
        }
      
        // 记录变更
        changes = append(changes, headingChange{
            lineStart: findLineStart(regionMap.Source, heading),
            oldLevel:  oldLevel,
            newLevel:  newLevel,
        })
        return ast.WalkContinue, nil
    })
  
    // 按行位置从后向前应用变更（避免偏移失效）
    return applyHeadingChanges(regionMap.Source, changes), nil
}
```

**只处理 ATX 标题（`#` 开头）：** Setext 标题（下划线 `===` / `---`）在语法层面只能表达 h1 和 h2，级别调整语义模糊。第一版对 Setext 标题不处理并输出 warning。

### 6.6 indent — 段首缩进

**功能：** 在正文段落的第一行行首插入两个全角空格 (U+3000)。

**可处理的区域：** 默认仅 RegionText。可通过 `scope` 扩展到 RegionBlockquote。

**不处理：**
- RegionHeading、RegionListItem、RegionTable、RegionCode、RegionHTML、RegionFrontMatter
- 已经以 U+3000 开头的行（幂等性）
- 空行

**段落第一行识别：** 一个 Paragraph 可能有软换行跨多行。只在 Paragraph 节点起始位置所在的行插入缩进。

**为什么用 U+3000：** ASCII 空格会被 Markdown 渲染器当作代码块缩进或直接忽略。U+3000 不被 Markdown 语法识别，在 GitHub、Typora、Obsidian、VS Code 预览等主流渲染器中都能保留为视觉缩进。

### 6.7 pangu — 盘古之白

**功能：** 中文字符和半角英文字母/数字之间自动插入空格。

**可处理的区域：** RegionText、RegionHeading、RegionListItem、RegionTable、RegionBlockquote。

**跳过：** RegionCode、RegionHTML、RegionFrontMatter、RegionEmphasisMarker。

**插入规则：**

```
逐字符扫描相邻字符对:

  if 当前是中文 且 下一个是 [a-zA-Z0-9]: 之间插空格
  if 当前是 [a-zA-Z0-9] 且 下一个是中文: 之间插空格
  if 当前是中文 且 下一个是 `:               之间插空格（行内代码起始）
  if 当前是 ` 且 下一个是中文:               之间插空格（行内代码结束）
  if 相邻位置已有空格: 不重复插入
```

**"中文字符"范围：**

| Unicode | 名称 |
|---------|------|
| U+4E00..U+9FFF | CJK Unified Ideographs |
| U+3400..U+4DBF | CJK Extension A |
| U+F900..U+FAFF | CJK Compatibility |
| U+3000..U+303F | CJK 标点 |
| U+FF00..U+FFEF | 全角字母数字 |
| U+201C..U+201D | 中文双引号 " " |
| U+2018..U+2019 | 中文单引号 ' ' |

**中文标点与 ASCII 的规则：**
- 中文标点（含弯引号）和 ASCII 字母/数字之间：插入空格
- 中文标点和中文之间：不插入
- ASCII 标点和任何字符之间：不插入

### 6.8 emphasis_space — 强调标记空格

**功能：** `**text**`、`*text*`、`***text***` 前后如果紧邻中文，插入空格。

**实现思路：** 不自己用正则解析 Markdown 强调语法（边界情况太多），而是利用 RegionEmphasisMarker 区域信息。构建 RegionMap 时，遍历 AST 的 Emphasis 节点，记录其 open/close delimiter 的字节位置。

```
对每个 RegionEmphasisMarker:
  if 是 open 标记（左侧）:
    if 前一字符是中文 且 不是空格: 前面插空格
  if 是 close 标记（右侧）:
    if 后一字符是中文 且 不是空格: 后面插空格
```

**与 pangu 的交互：** pangu 可能已在强调标记旁插入了空格（如果标记内容开头/结尾是英文）。两个规则都要检查"已有空格则不重复插入"。emphasis_space 在 pangu 之后执行。

---

## 7. LLM 处理系统

### 7.1 核心交互模式

**统一模式：整篇输入 + 结构化输出 + 程序回贴。**

```
┌──────────────────────────────────────────┐
│              程序：准备阶段               │
│  原文 → 标注段落号/标题号 → 构造 prompt   │
└──────────────┬───────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────┐
│              LLM：处理阶段               │
│  接收完整标注原文 + 指令                  │
│  返回 JSON（id + prefix + 内容）         │
└──────────────┬───────────────────────────┘
               │
               ▼
┌──────────────────────────────────────────┐
│              程序：回贴阶段               │
│  解析 JSON → id + prefix 双重校验         │
│  → 替换对应节点                          │
└──────────────────────────────────────────┘
```

**关键：原文在一个 step 内保持不变**，使 LLM API 的 prefix cache 能跨轮次复用（例如同一 step 内的 detect + rewrite 两轮）。跨 step 之间原文会改变，此时缓存失效。

**为什么串行执行 LLM step？**

`llm.concurrency: 1` 默认串行。原因：
- 用户旅程中的多个 step 通常有先后依赖（先粗改再精修）
- 串行可以让每个 step 内的多轮调用共享 prefix cache
- 并行调用同一份原文的多个 step 会导致合并冲突

### 7.2 文档标注

发给 LLM 之前，程序对原文进行结构标注，为可寻址节点分配编号。

**标注格式示例：**

```markdown
[P1] 这是第一个段落的内容。

[H1] # 主标题

[P2] 主标题下的段落。

[H2] ## 副标题：一个冗长的标题

[P3] 副标题下的段落。

[H3] ## 性能优化——从理论到实践
```

**标注规则：**
- `[Pn]` — 段落
- `[Hn]` — 标题
- `[Ln]` — 列表项
- `[Qn]` — 引用块

各类型独立编号，从 1 开始递增。

**标注只用于 LLM 输入，最终输出不包含标注。**

```go
type AnnotatedDoc struct {
    Source    []byte             // 原始文本（不带标注）
    Annotated []byte             // 带标注的文本（发给 LLM）
    Nodes     []AnnotatedNode
    NodeByID  map[string]*AnnotatedNode
}

type AnnotatedNode struct {
    ID     string    // "P1", "H3" 等
    Type   string    // "paragraph", "heading" 等
    Start  int       // 原文中的起始字节偏移
    End    int       // 原文中的结束字节偏移
    Text   string    // 节点原始文本
    Prefix string    // 前 15 个 UTF-8 字符（用于匹配校验）
    Level  int       // 仅 heading
}
```

### 7.3 两种 Action

#### 7.3.1 rewrite

**执行流程：**

```
1. 标注文档
2. 确定目标节点：
   if detect: true:
     检测轮 → LLM 返回目标节点 ID 列表
   else:
     程序侧 filter 筛选
3. 改写轮：整篇 + 目标节点列表 → LLM 返回 JSON
4. 回贴：id + prefix 校验后替换
```

**detect 字段的选择：**

| 场景 | detect | 原因 |
|------|--------|------|
| 去掉转折对比句式 | true | 需要 LLM 理解语义找出所有变体 |
| 重写标题 | false | 短、结构明确，程序 filter 足够 |
| 段落润色 | false | 全部段落都要处理 |

**为什么标题不需要 detect：** 标题短、结构清晰，`filter.level` 就能精确圈定范围。至于标题内容是否需要改，交给改写轮的 prompt 说明——LLM 会判断哪些标题符合"需要改写"的模式，不需要的原样返回。

#### 7.3.2 full_rewrite

**执行流程：**

```
1. 完整原文作为 document 变量
2. 渲染 prompt 模板
3. 单次调用 LLM（不用 JSON mode）
4. LLM 返回完整改写文档，直接替换
```

**特点：**
- 不做标注（不需要定位回贴）
- 不用 JSON 输出
- 单次调用
- 失败时跳过此 step，用上一步的输出继续

### 7.4 JSON 契约

#### 7.4.1 检测轮输出

```json
{
  "matches": [
    {"id": "P3", "prefix": "这并不是说我们不应"},
    {"id": "P7", "prefix": "与其关注表面的数据"}
  ]
}
```

#### 7.4.2 改写轮输出

```json
{
  "rewrites": [
    {"id": "P3", "prefix": "这并不是说我们不应", "text": "改写后的完整段落..."},
    {"id": "P7", "prefix": "与其关注表面的数据", "text": "改写后的完整段落..."}
  ]
}
```

字段说明：
- `id`：节点标注 ID
- `prefix`：原始节点文本的前 15 个字符（校验用）
- `text`：改写后的完整节点文本

#### 7.4.3 JSON 解析容错

| 异常 | 处理 |
|------|------|
| JSON 语法错误 | 尝试修复常见问题（尾逗号、Markdown 代码块包裹）；失败则记录错误后按 `on_failure` 处理 |
| 返回 ```json ... ``` 包裹 | 自动剥离 |
| id 不存在 | 跳过该条 + warning |
| prefix 不匹配 | 跳过该条 + warning |
| text 为空 | 跳过该条 + warning |
| 返回未请求的节点（LLM 多改了） | 跳过该条 + warning |
| 缺失请求的节点（LLM 少改了） | 视为该节点不需改，不 warning |

### 7.5 回贴逻辑

```go
func applyRewrites(doc *AnnotatedDoc, rewrites []Rewrite) ([]byte, []Warning) {
    var warnings []Warning
  
    // 按节点在原文的位置从后向前排序
    sort.Slice(rewrites, func(i, j int) bool {
        ni := doc.NodeByID[rewrites[i].ID]
        nj := doc.NodeByID[rewrites[j].ID]
        return ni.Start > nj.Start
    })
  
    result := append([]byte{}, doc.Source...)
  
    for _, rw := range rewrites {
        node, ok := doc.NodeByID[rw.ID]
        if !ok {
            warnings = append(warnings, warn("节点 %s 不存在", rw.ID))
            continue
        }
      
        // prefix 校验
        if !strings.HasPrefix(node.Text, rw.Prefix) {
            warnings = append(warnings, warn(
                "节点 %s prefix 不匹配（期望 '%s'，实际 '%s'）",
                rw.ID, rw.Prefix, node.Prefix,
            ))
            continue
        }
      
        result = append(
            result[:node.Start],
            append([]byte(rw.Text), result[node.End:]...)...,
        )
    }
  
    return result, warnings
}
```

**从后向前替换：** 保证前面替换不影响后面节点的偏移量。

### 7.6 LLM Step 配置结构

```yaml
llm_steps:
  # 示例 1：检测 + 改写（两轮）
  - name: "去掉转折对比句式"
    action: rewrite
    target: paragraph                  # 节点类型
    detect: true                       # 启用检测轮
    prompt:
      detect: detect_contrasting
      rewrite: rewrite_contrasting
    params:                            # 传给 prompt 的变量
      style: "直接陈述"
    overrides:                         # 覆盖 provider 默认参数
      temperature: 0.7

  # 示例 2：直接改写（单轮，程序 filter）
  - name: "重写标题"
    action: rewrite
    target: heading
    detect: false
    filter:
      level: [1, 2, 3]
    prompt:
      rewrite: rewrite_heading

  # 示例 3：全文改写
  - name: "全文细化"
    action: full_rewrite
    prompt:
      rewrite: elaborate_steps
    params:
      expand_ratio: "1.3"
```

### 7.7 target 与 filter

**target**（必填）：`paragraph` | `heading` | `list_item` | `blockquote`

**filter**（可选）：

```yaml
filter:
  level: [1, 2, 3]        # 仅 heading：只选这些级别
  min_length: 30           # 最少字符数（UTF-8 计算）
  max_length: 2000
```

**设计约定：** filter 只做结构层面的硬筛选，不做内容模式匹配。内容判断交给 prompt 让 LLM 做——LLM 有能力判断哪些节点需要改，不需要就原样返回。

filter 的作用：**减少发给 LLM 的节点数量，控制成本。** 例如"改写标题"时用 `level: [1, 2, 3]` 排除掉四级以下的标题。

### 7.8 提示词的自动注入变量

| 变量 | 何时注入 | 内容 |
|------|----------|------|
| `annotated_doc` | rewrite action | 带标注的完整原文 |
| `document` | full_rewrite action | 不带标注的完整原文 |
| `target_nodes` | rewrite 改写轮 | 目标节点的格式化列表，如 `[P3] 这并不是说我们不应...\n[P7] 与其关注表面的...` |

`target_nodes` 由程序生成，格式统一：

```
[ID1] 节点前 50 个字符（不足则完整）...
[ID2] 节点前 50 个字符...
```

用 50 个字符（而不是完整文本）是因为完整文本 LLM 已经在 `annotated_doc` 中看到了，这里只是告诉 LLM "这些是要改的"。

### 7.9 LLM Step 完整执行流程

```go
func (s *LLMStep) Execute(doc []byte, provider Provider) ([]byte, error) {
    switch s.Action {
    case ActionRewrite:
        return s.executeRewrite(doc, provider)
    case ActionFullRewrite:
        return s.executeFullRewrite(doc, provider)
    }
    return nil, fmt.Errorf("unknown action")
}

func (s *LLMStep) executeRewrite(doc []byte, provider Provider) ([]byte, error) {
    // 1. 标注
    annotated := annotateDocument(doc, s.Target)
  
    // 2. 确定目标节点
    var targets []AnnotatedNode
  
    if s.Detect {
        // 检测轮
        detectVars := map[string]any{"annotated_doc": string(annotated.Annotated)}
        mergeVars(detectVars, s.Params)
        detectPrompt, err := s.Prompt.Detect.Render(detectVars)
        if err != nil { return nil, err }
      
        detectResp, err := provider.Complete(&CompletionRequest{
            Messages: []Message{{Role: "user", Content: detectPrompt}},
            ResponseFormat: FormatJSON,
        })
        if err != nil { return nil, err }
      
        matches, err := parseDetectResponse(detectResp.Content)
        if err != nil { return nil, err }
      
        targets = validateAndFilterMatches(annotated, matches)
    } else {
        // 程序 filter
        targets = applyFilter(annotated.Nodes, s.Filter)
    }
  
    if len(targets) == 0 {
        return doc, nil
    }
  
    // 3. 改写轮
    rewriteVars := map[string]any{
        "annotated_doc": string(annotated.Annotated),
        "target_nodes":  formatTargets(targets),
    }
    mergeVars(rewriteVars, s.Params)
  
    rewritePrompt, err := s.Prompt.Rewrite.Render(rewriteVars)
    if err != nil { return nil, err }
  
    rewriteResp, err := provider.Complete(&CompletionRequest{
        Messages: []Message{{Role: "user", Content: rewritePrompt}},
        ResponseFormat: FormatJSON,
    })
    if err != nil { return nil, err }
  
    rewrites, err := parseRewriteResponse(rewriteResp.Content)
    if err != nil { return nil, err }
  
    // 4. 回贴
    result, warnings := applyRewrites(annotated, rewrites)
    for _, w := range warnings { log.Warn(w) }
  
    return result, nil
}

func (s *LLMStep) executeFullRewrite(doc []byte, provider Provider) ([]byte, error) {
    vars := map[string]any{"document": string(doc)}
    mergeVars(vars, s.Params)
  
    prompt, err := s.Prompt.Rewrite.Render(vars)
    if err != nil { return nil, err }
  
    resp, err := provider.Complete(&CompletionRequest{
        Messages: []Message{{Role: "user", Content: prompt}},
        // 不用 JSON mode
    })
    if err != nil { return nil, err }
  
    return []byte(resp.Content), nil
}
```

### 7.10 失败处理

**单轮失败（LLM 调用错误）：**

| on_failure | 行为 |
|------------|------|
| preserve | 跳过当前 step，返回输入原样，warning |
| fail | 立即终止流水线 |

**JSON 解析失败：** 
- 尝试修复（剥离 Markdown 包裹、修复尾逗号）
- 仍失败 → 按 on_failure 处理

**部分节点失败（prefix 不匹配等）：**
- 该节点保留原样，其他节点正常处理
- warning 记录

**连续失败熔断：**
- 如果同一 step 中连续 3 次 LLM 调用失败，即使 `on_failure: preserve` 也终止整个流水线
- 防止 API key 失效等情况下静默跳过所有 LLM 步骤

### 7.11 Provider 抽象

```go
type Provider interface {
    Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error)
}

type CompletionRequest struct {
    Model          string
    Messages       []Message
    Temperature    float64
    MaxTokens      int
    ResponseFormat ResponseFormat  // FormatText | FormatJSON
}

type Message struct {
    Role    string  // "system" | "user" | "assistant"
    Content string
}

type CompletionResponse struct {
    Content      string
    FinishReason string
    Usage        Usage
}

type Usage struct {
    PromptTokens     int
    CompletionTokens int
    TotalTokens      int
    CachedTokens     int  // prefix cache 命中的 token 数（如果 provider 支持）
}
```

**三种内置 Provider：**

| Provider | 端点 | JSON mode |
|----------|------|-----------|
| openai | `POST {base_url}/chat/completions` | `response_format: {type: "json_object"}` |
| anthropic | `POST {base_url}/v1/messages` | 通过 prompt 中要求 JSON 输出 + 后处理 |
| ollama | `POST {base_url}/api/chat` | `format: "json"` |

**HTTP 客户端：**

- 超时（`timeout`）
- 指数退避重试（429 / 5xx，最多 `max_retries` 次）
- 速率限制（golang.org/x/time/rate）

---

## 8. 提示词系统

### 8.1 提示词文件格式

Markdown 文件 + YAML front matter：

```markdown
---
name: rewrite_contrasting
description: 改写转折对比句式段落
version: 1

params:
  required:
    - annotated_doc
    - target_nodes
  optional:
    style:
      default: "直接陈述事实，避免转折对比"
      description: 目标表达风格

suggested:
  temperature: 0.7
  response_format: json
---

下面是一篇文档，每个段落前标注了编号：

{{ .annotated_doc }}

请改写以下段落，去除"不是 A 而是 B"、"并非……而是……"等转折对比句式，改为更直接的表达。

目标段落：
{{ .target_nodes }}

风格要求：{{ .style }}

以 JSON 格式输出：
```json
{
  "rewrites": [
    {"id": "段落编号", "prefix": "原段落前15字符", "text": "改写后完整段落"}
  ]
}
```

只输出 JSON，不要解释。
```

### 8.2 Front Matter 字段

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `name` | string | 是 | 唯一标识符 |
| `description` | string | 是 | 一句话描述 |
| `version` | int | 是 | 版本号 |
| `params.required` | []string | 是 | 必填变量列表 |
| `params.optional` | map | 否 | 可选变量及默认值 |
| `suggested.temperature` | float | 否 | 推荐温度 |
| `suggested.response_format` | string | 否 | `text` 或 `json`，影响 provider 的 response_format 参数 |

### 8.3 模板变量注入优先级

从低到高：

```
1. Prompt 文件 optional 参数的 default 值
2. Rule 自动注入的变量（annotated_doc / document / target_nodes）
3. Step 配置的 params 字段
```

高优先级覆盖低优先级。必填变量缺失时渲染报错。

### 8.4 提示词加载

**启动时流程：**

```
1. 加载内置提示词（go:embed builtin/*.md），注册到 registry
2. 按 llm.prompts.dirs 数组顺序扫描
3. 每个目录递归查找 *.md
4. 解析 front matter 和模板正文
5. 注册到 registry（同名覆盖）
```

**覆盖优先级：** dirs 数组中越靠前优先级越高。项目 `./prompts` > 用户 `~/.config/dellm/prompts` > 内置。

### 8.5 Prompt Registry 接口

```go
type Prompt struct {
    Name        string
    Description string
    Version     int
    Params      ParamSpec
    Suggested   SuggestedParams
    Template    *template.Template
    Source      string    // 文件路径或 "[builtin]"
}

type ParamSpec struct {
    Required []string
    Optional map[string]ParamDef
}

type ParamDef struct {
    Default     any
    Description string
}

type SuggestedParams struct {
    Temperature    *float64
    ResponseFormat string  // "text" | "json"
}

func (p *Prompt) Render(vars map[string]any) (string, error)

type Registry struct { ... }

func (r *Registry) Load(builtinFS embed.FS, dirs []string) error
func (r *Registry) Get(name string) (*Prompt, bool)
func (r *Registry) List() []*PromptInfo
```

### 8.6 内置提示词清单

| 名称 | 用途 | 用于哪个 action |
|------|------|-----------------|
| `detect_contrasting` | 检测转折对比句式 | rewrite 的 detect 轮 |
| `rewrite_contrasting` | 改写转折对比句式 | rewrite 的改写轮 |
| `rewrite_heading` | 重写标题 | rewrite 的改写轮（无 detect） |
| `elaborate_steps` | 全文细化 | full_rewrite |
| `compress` | 压缩文本 | full_rewrite |
| `polish` | 段落润色 | rewrite 的改写轮 |

内容见附录。

---

## 9. Pipeline 编排

### 9.1 Pipeline 结构

```go
type Pipeline struct {
    preprocess  []rule.Rule
    llmSteps    []LLMStep
    postprocess []rule.Rule
    config      *config.Config
}

type LLMStep struct {
    Name       string
    Action     ActionType
    Target     string
    Detect     bool
    Filter     *Filter
    Prompt     PromptRef        // { Detect, Rewrite }
    Params     map[string]any
    Overrides  ProviderOverrides
    Provider   Provider          // 从 config.llm.providers 解析
}

type PromptRef struct {
    Detect  *Prompt   // 可为 nil（detect: false 时）
    Rewrite *Prompt   // 必填
}
```

### 9.2 执行主流程

```go
func (p *Pipeline) Process(source []byte) ([]byte, error) {
    current := source
  
    // ① 预处理
    for _, r := range p.preprocess {
        regionMap := buildRegionMap(current)
        result, err := r.Transform(regionMap)
        if err != nil {
            return nil, fmt.Errorf("preprocess %s: %w", r.Name(), err)
        }
        current = result
    }
  
    // ② LLM 步骤
    consecutiveFailures := 0
    for _, step := range p.llmSteps {
        result, err := step.Execute(current, step.Provider)
        if err != nil {
            consecutiveFailures++
            if consecutiveFailures >= 3 {
                return nil, fmt.Errorf("llm step %q: 连续 3 次失败终止", step.Name)
            }
            if p.config.LLM.OnFailure == "fail" {
                return nil, fmt.Errorf("llm step %q: %w", step.Name, err)
            }
            log.Warnf("llm step %q failed: %v, 保留原文继续", step.Name, err)
            continue
        }
        consecutiveFailures = 0
        current = result
    }
  
    // ③ 后处理
    for _, r := range p.postprocess {
        regionMap := buildRegionMap(current)
        result, err := r.Transform(regionMap)
        if err != nil {
            return nil, fmt.Errorf("postprocess %s: %w", r.Name(), err)
        }
        current = result
    }
  
    return current, nil
}
```

### 9.3 Pipeline 构建流程

```
1. 加载配置，确定使用的 profile
2. 应用 --only / --skip / --skip-llm / --shift-headings 的过滤和调整
3. 对 profile.preprocess 中的每个规则名:
   a. 从 rule.Registry 获取工厂函数
   b. 从 config.rules 获取参数
   c. 创建 Rule 实例
4. 对 profile.llm_steps 中的每个步骤:
   a. 从 prompt.Registry 获取 detect/rewrite prompt
   b. 从 config.llm.providers 获取 provider（应用 overrides）
   c. 构造 LLMStep
5. 对 profile.postprocess 同步骤 3
6. 组装为 Pipeline
```

### 9.4 --dry-run 行为

```
$ dellm input.md -p full --dry-run

配置: dellm.yaml (profile: full)
输入: input.md (12.3 KB, 47 段落, 15 标题)

预处理:
  [1] remove_separators
      预计删除: 3 处分隔线

LLM 步骤:
  [1] "全文细化" (full_rewrite)
      模型: gpt-4o
      预估 token: ~12,000 (输入) + ~15,600 (输出)
  [2] "重写标题" (rewrite, detect=false)
      模型: gpt-4o
      filter 筛出: 8 个标题（h1-h3）
      预估 token: ~13,500 (输入) + ~800 (输出)  [prefix cache 命中率高]
  [3] "去掉转折对比句式" (rewrite, detect=true)
      模型: gpt-4o
      检测轮预估: ~13,500 (输入) + ~200 (输出)
      改写轮预估: ~13,700 (输入) + ~1,500 (输出)  [prefix cache 命中率高]

  总预估 token: ~70,800
  预估费用: ~$0.35 (基于 gpt-4o 定价)

后处理:
  [1] quotes
  [2] indent
  [3] pangu
  [4] emphasis_space
```

**token 预估方法：**
- 中文 1 字符 ≈ 1.5 token
- 英文 4 字符 ≈ 1 token
- full_rewrite 输出按输入的 `expand_ratio` 倍计算
- rewrite 检测轮输出按输入的 5% 估算
- rewrite 改写轮输出按目标节点总字符数的 1.5 倍估算

---

## 10. 错误处理与日志

### 10.1 日志级别

| 级别 | 内容 |
|------|------|
| error | 致命错误 |
| warn | 未闭合引号、LLM 单节点失败、prefix 不匹配等 |
| info | 处理进度：当前文件、当前步骤、匹配节点数 |
| debug | Region 详情、prompt 渲染结果、LLM 请求/响应 |

默认 `info`。`-v` 切换到 `debug`。

日志输出到 stderr，不污染 stdout。

### 10.2 退出码

| 退出码 | 含义 |
|--------|------|
| 0 | 成功 |
| 1 | 处理失败 |
| 2 | 配置错误 |

### 10.3 Stats 输出

```
── 处理统计 ──────────────────────────
文件: input.md

预处理:
  remove_separators:   删除 3 处分隔线

LLM 步骤:
  "全文细化":            成功
    Token: 11,800 (输入) + 14,500 (输出), 缓存命中: 0
  "重写标题":            处理 8 个节点，成功 8
    Token: 27,000 (输入) + 800 (输出), 缓存命中: 13,000
  "去掉转折对比句式":     检测 5 个节点，改写成功 5
    Token: 27,500 (输入) + 1,700 (输出), 缓存命中: 26,800

后处理:
  quotes:               替换 24 处引号
  indent:               缩进 15 个段落
  pangu:                插入 47 处空格
  emphasis_space:       插入 8 处空格

总 Token: 66,300 (输入) + 17,000 (输出)
缓存命中: 39,800 tokens (60.0%)
耗时: 52.3s (LLM: 51.1s, 本地: 1.2s)
──────────────────────────────────────
```

---

## 11. 扩展性设计

### 11.1 添加新的本地规则

1. 在 `rule/` 下创建新文件，实现 `Rule` 接口
2. 在 `rule/registry.go` 中注册工厂函数
3. 在配置 `rules:` 中添加参数定义
4. 在需要的 profile 的 preprocess/postprocess 中引用

无需改动 Pipeline、CLI、配置加载代码。

### 11.2 添加新的 LLM Provider

1. 在 `llm/providers/` 下创建新文件，实现 `Provider` 接口
2. 在 provider 工厂中注册新的 type
3. 用户配置 `type: new_provider` 即可

### 11.3 添加新的提示词

- 内置：在 `llm/prompt/builtin/` 下添加 `.md` 文件
- 用户：在用户目录或项目目录下添加，无需改代码

在配置的 `llm_steps` 中通过 `prompt: <name>` 引用。

### 11.4 添加新的 Action 类型

在 `llm/action.go` 中定义新的 ActionType，实现执行函数，在 `LLMStep.Execute` switch 中添加分支。

预留扩展：
- `detect_only`：只检测不改写，输出诊断报告
- `interactive`：逐个节点展示 LLM 建议，用户 approve/reject
- `classify`：让 LLM 对节点分类打标签

---

## 12. 测试策略

### 12.1 本地规则测试

Golden file 测试：

```
testdata/quotes/
├── basic_input.md
├── basic_expected.md
├── nested_input.md
├── nested_expected.md
├── code_block_input.md
├── code_block_expected.md
├── unclosed_input.md
└── unclosed_expected.md
```

```go
func TestQuotesRule(t *testing.T) {
    cases := []string{"basic", "nested", "code_block", "unclosed"}
    for _, name := range cases {
        t.Run(name, func(t *testing.T) {
            input := loadFile("testdata/quotes/" + name + "_input.md")
            expected := loadFile("testdata/quotes/" + name + "_expected.md")
            rule, _ := NewQuotesRule(defaultQuotesConfig)
            regionMap := buildRegionMap(input)
            result, err := rule.Transform(regionMap)
            require.NoError(t, err)
            assert.Equal(t, string(expected), string(result))
        })
    }
}
```

### 12.2 RegionMap 测试

验证 goldmark AST 解析产生的区域映射：

```go
func TestRegionMap(t *testing.T) {
    source := []byte("# Title\n\nHello `code` world\n\n```go\nfmt.Println(\"hi\")\n```\n")
    regions := buildRegionMap(source)
    assertRegion(t, regions, "Title", RegionHeading)
    assertRegion(t, regions, "Hello ", RegionText)
    assertRegion(t, regions, "code", RegionCode)
    assertRegion(t, regions, "fmt.Println(\"hi\")", RegionCode)
}
```

### 12.3 LLM 步骤测试

使用 mock provider：

```go
type MockProvider struct {
    responses []MockResponse
    idx       int
}

type MockResponse struct {
    MatchPromptContains string
    Content             string
    Err                 error
}

func (m *MockProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
    for _, r := range m.responses {
        if strings.Contains(req.Messages[len(req.Messages)-1].Content, r.MatchPromptContains) {
            if r.Err != nil { return nil, r.Err }
            return &CompletionResponse{Content: r.Content}, nil
        }
    }
    return nil, fmt.Errorf("no mock response")
}
```

测试点：
- Annotation 生成正确
- Filter 筛选正确
- JSON 解析容错
- prefix 校验
- 回贴位置正确
- 从后向前替换的偏移量正确

### 12.4 Pipeline 集成测试

端到端，LLM 用 mock：

```go
func TestPipelineEndToEnd(t *testing.T) {
    input := readFile("testdata/integration/input.md")
    expected := readFile("testdata/integration/expected.md")
    cfg := loadConfig("testdata/integration/config.yaml")
  
    pipeline := BuildPipeline(cfg, WithMockProvider(mockProvider))
    result, err := pipeline.Process(input)
    require.NoError(t, err)
    assert.Equal(t, string(expected), string(result))
}
```

### 12.5 Prompt 渲染测试

```go
func TestPromptRender(t *testing.T) {
    p := loadPrompt("rewrite_contrasting")
    result, err := p.Render(map[string]any{
        "annotated_doc": "[P1] 测试段落",
        "target_nodes":  "[P1] 测试段落",
        "style":         "克制",
    })
    require.NoError(t, err)
    assert.Contains(t, result, "克制")
    assert.Contains(t, result, "[P1] 测试段落")
}
```

---

## 13. 风险与已知限制

| 编号 | 项目 | 描述 | 应对 |
|------|------|------|------|
| R1 | goldmark AST 偏移精度 | 需验证 goldmark 的 AST 节点是否精确返回字节偏移，特别是围栏代码块和 emphasis 标记 | 编码开始前写 spike 测试验证 |
| R2 | 单引号撇号启发式 | "前后都是 ASCII 字母则视为撇号"对 `'twas` 会误判 | 可接受，极少见 |
| R3 | LLM 幻觉 id | LLM 可能返回不存在的节点 id | prefix 双重校验 + warning 跳过 |
| R4 | LLM JSON 格式污染 | 可能返回 Markdown 包裹的 JSON、多余解释 | 解析容错：剥离代码块、修复尾逗号 |
| R5 | full_rewrite 结构漂移 | LLM 全文重写可能改变标题层级、列表样式 | Prompt 明确要求保留结构；无法完美防御 |
| R6 | 幂等性 | 反复运行 LLM 步骤会反复改写同一内容 | 用户自觉；未来可加内容 hash 缓存 |
| R7 | Prefix cache 依赖 | 依赖 LLM API 支持 prefix cache 才能省钱 | OpenAI/Anthropic/DeepSeek 都支持；不支持的 provider 也能跑，只是不省 |
| R8 | 标题标注污染 | `[H1] # 标题` 这种标注可能被 LLM 误认为是标题的一部分 | Prompt 中明确解释标注的含义；改写轮的输出 text 字段要求包含完整标题 `# 标题` |
| R9 | 段落跨行的 prefix 提取 | 段落可能包含软换行，前 15 字符如果跨行怎么处理 | 提取时忽略换行符，只取可见字符 |
| R10 | Setext 标题 shift 语义 | Setext 语法只能表达 h1/h2，shift 到 h3+ 需要转 ATX | 第一版对 Setext 标题不处理并 warning |

---

## 14. 附录：内置提示词全文

### 14.1 detect_contrasting.md

```markdown
---
name: detect_contrasting
description: 检测"不是A而是B"等转折对比句式
version: 1
params:
  required: [annotated_doc]
suggested:
  temperature: 0.3
  response_format: json
---

下面是一篇 Markdown 文档，每个段落前标注了编号（如 [P1]、[P2]）：

{{ .annotated_doc }}

请找出上面文档中所有使用了以下转折对比句式的段落：
- "不是 A 而是 B"、"不是……而是……"
- "并非……而是……"、"并不是……而是……"
- "与其……不如……"
- "关键不在于……而在于……"
- "重要的不是……而是……"
- 语义相似的所有变体

以 JSON 格式输出：

```json
{
  "matches": [
    {"id": "段落编号（如 P3）", "prefix": "段落前 15 个字符"}
  ]
}
```

如果没有找到，输出：`{"matches": []}`。只输出 JSON，不要解释。
```

### 14.2 rewrite_contrasting.md

```markdown
---
name: rewrite_contrasting
description: 改写转折对比句式段落
version: 1
params:
  required: [annotated_doc, target_nodes]
  optional:
    style:
      default: "直接陈述事实，避免无意义的转折对比"
suggested:
  temperature: 0.7
  response_format: json
---

下面是一篇 Markdown 文档：

{{ .annotated_doc }}

请改写以下段落，去除"不是 A 而是 B"、"并非……而是……"等转折对比句式，改为更直接的表达。

目标段落：
{{ .target_nodes }}

风格要求：{{ .style }}

以 JSON 格式输出：

```json
{
  "rewrites": [
    {"id": "段落编号", "prefix": "原段落前 15 字符", "text": "改写后的完整段落文本"}
  ]
}
```

注意：
- `text` 是改写后的完整段落，不要加编号标注
- 不要保留原文中的转折对比结构
- 如果某个目标段落改写后你觉得反而更差，可以不返回该条

只输出 JSON。
```

### 14.3 rewrite_heading.md

```markdown
---
name: rewrite_heading
description: 重写标题，去除冒号和破折号分隔模式
version: 1
params:
  required: [annotated_doc, target_nodes]
  optional:
    style:
      default: "简短有力，4-15 个汉字，不用冒号或破折号连接并列概念"
suggested:
  temperature: 0.5
  response_format: json
---

下面是一篇 Markdown 文档，每个段落和标题前标注了编号：

{{ .annotated_doc }}

请检查以下标题，如果它们符合以下模式，改写为更简洁的形式：
- "主题：副标题"结构
- "主题——副标题"或"主题-副标题"结构
- 过于冗长（超过 15 汉字）
- 明显的 AI 味套路（如"深入浅出 XX"、"XX 的艺术"）

如果标题已经足够简洁自然，请**不要**返回该标题的改写。

目标标题：
{{ .target_nodes }}

风格要求：{{ .style }}

以 JSON 格式输出：

```json
{
  "rewrites": [
    {"id": "标题编号", "prefix": "原标题前 15 字符", "text": "改写后的完整标题（含 # 号）"}
  ]
}
```

注意：
- `text` 字段必须包含完整的 Markdown 标题语法，如 `## 简洁标题`
- 保持原有的标题级别（# 的数量不变）
- 只返回真正需要改写的标题

只输出 JSON。
```

### 14.4 elaborate_steps.md

```markdown
---
name: elaborate_steps
description: 全文细化，补充跳过的步骤和推理过程
version: 1
params:
  required: [document]
  optional:
    expand_ratio:
      default: "1.3"
      description: 目标扩展倍数
    style:
      default: "技术文档风格，先说结论再展开步骤"
suggested:
  temperature: 0.5
---

请重写下面的 Markdown 文档。保留所有原有的事实、结论、代码和结构，但：

1. 补充被跳过的中间步骤和推理过程
2. 在每个主要步骤前后添加必要的背景说明和过渡
3. 文档长度扩展到原来的约 {{ .expand_ratio }} 倍
4. 风格：{{ .style }}

严格要求：
- 完整保留 Markdown 结构（标题层级、列表、代码块、表格）
- 不要删除原有信息
- 不要编造原文没有的具体数据或代码
- 只补充解释、背景、步骤衔接

原文档：

{{ .document }}

直接输出重写后的完整 Markdown 文档，不要加任何前后缀或解释。
```

### 14.5 compress.md

```markdown
---
name: compress
description: 压缩文本，去除冗余，保留核心信息
version: 1
params:
  required: [document]
  optional:
    ratio:
      default: "0.6"
      description: 目标压缩比
suggested:
  temperature: 0.3
---

请压缩下面的 Markdown 文档，去除冗余表达和废话，保留所有核心信息。

要求：
- 目标长度：原文的约 {{ .ratio }} 倍
- 完整保留 Markdown 结构（标题、列表、代码块）
- 不删除任何事实性内容
- 只删除冗余的形容词、重复表达、无信息量的过渡句

原文档：

{{ .document }}

直接输出压缩后的完整文档。
```

### 14.6 polish.md

```markdown
---
name: polish
description: 段落润色，提升流畅度和可读性
version: 1
params:
  required: [annotated_doc, target_nodes]
  optional:
    style:
      default: "流畅自然，避免书面腔和翻译腔"
suggested:
  temperature: 0.6
  response_format: json
---

下面是一篇文档：

{{ .annotated_doc }}

请润色以下段落，提升文字流畅度和可读性，但不要改变原意。

目标段落：
{{ .target_nodes }}

风格要求：{{ .style }}

以 JSON 格式输出：

```json
{
  "rewrites": [
    {"id": "段落编号", "prefix": "原段落前 15 字符", "text": "润色后的完整段落"}
  ]
}
```

只输出 JSON。
```

---

## 15. 附录：用户旅程示例

**场景：** 拿到 LLM 输出的中文技术文档，做完整清理。

**LLM 输出的问题：**
- 从 `##` 开始输出（应该从 `#` 开始）
- 有多个"不是A而是B"句式的段落
- 有些标题是"性能优化：从理论到实践"这种模式
- 内容跳步骤，有些地方讲得太简略
- 半角引号、中英文之间没空格

**配置：**

```yaml
# dellm.yaml
version: 1

profiles:
  full:
    preprocess:
      - shift_headings
      - remove_separators

    llm_steps:
      - name: "全文细化"
        action: full_rewrite
        prompt: { rewrite: elaborate_steps }
        params: { expand_ratio: "1.3" }

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

rules:
  shift_headings:
    offset: -1
    on_overflow: clamp

llm:
  providers:
    default:
      type: openai
      base_url: "https://api.deepseek.com/v1"
      api_key: "${DEEPSEEK_API_KEY}"
      model: "deepseek-chat"
      temperature: 0.5
```

**使用：**

```bash
export DEEPSEEK_API_KEY=xxx

# 预览要做什么
dellm article.md -p full --dry-run

# 实际处理
dellm article.md -p full -i --stats
```

**执行顺序：**

1. `shift_headings`：所有标题级别 -1（`##` → `#`）
2. `remove_separators`：删除分隔线
3. LLM 步骤 1（`full_rewrite`）：整篇细化，补充跳过的步骤
4. LLM 步骤 2（`rewrite` heading）：把所有 h1-h3 标题送给 LLM，LLM 判断并改写含冒号/破折号的
5. LLM 步骤 3（`rewrite` paragraph, detect=true）：LLM 先检测转折对比句式的段落，再逐个改写
6. `quotes`：半角引号 → 中文弯引号
7. `indent`：段首加两个全角空格
8. `pangu`：中英文间加空格
9. `emphasis_space`：`**` 前后加空格
