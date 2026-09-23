# DeLLM 设计文档 v1.1

> 基于《CLI应用架构之禅》重构

## 0. 架构哲学

DeLLM 遵循《CLI应用架构之禅》中提炼的核心原则：

- **稳定的核心，灵活的边缘**：核心数据模型（Markdown AST、AnnotatedDoc）一旦确定就不轻易变动；规则和 LLM 步骤作为边缘组件可自由增减
- **接口优于实现**：Rule 接口、Provider 接口、Prompt 接口的稳定性远超任何具体规则的实现
- **尊重组合**：尊重 stdin/stdout/stderr，提供机器可解析输出（--format json），支持管道
- **为演进而设计**：插件化的规则系统、提示词目录扫描、外部子命令模式
- **将 CLI 视为库的壳**：核心逻辑封装为可独立测试的包，CLI 层只是薄壳

---

## 1. 项目定位

DeLLM 是一个命令行工具，用于去除 LLM 生成的中文 Markdown 文档中的"AI 味"。通过本地规范化规则和可编排的 LLM 改写步骤，将 LLM 输出转化为符合中文排版习惯、风格自然的文档。

**技术选型：**

| 组件 | 选择 | 理由 |
|------|------|------|
| 语言 | Go 1.22+ | 单二进制分发，快速启动，跨平台 |
| Markdown 解析 | goldmark | AST 结构清晰，模块化，活跃维护 |
| CLI 框架 | cobra | Go 社区标准，子命令支持完善 |
| 配置管理 | viper | 多层级合并、环境变量展开 |
| 日志 | slog (标准库) | 结构化日志，零依赖 |

---

## 2. 整体架构

### 2.1 分层架构

遵循"CLI 应用架构总览"的分层模式：

```
dellm/
├── cmd/                              # 入口层
│   ├── main.go                       # 信号注册、全局初始化
│   └── root.go                       # cobra 根命令、子命令路由
│
├── internal/command/                 # 命令层（薄壳）
│   ├── process.go                    # 主处理命令
│   ├── list_rules.go
│   ├── list_prompts.go
│   └── version.go
│
├── pkg/                              # 核心层（库，可被外部引用）
│   ├── core/                         # 核心数据模型
│   │   ├── document.go               # Document 中间表示
│   │   ├── region.go                 # RegionMap
│   │   └── annotated.go              # AnnotatedDoc
│   │
│   ├── parser/                       # Markdown 解析（goldmark 封装）
│   │   ├── parser.go
│   │   └── annotate.go
│   │
│   ├── rule/                         # 规则系统
│   │   ├── rule.go                   # Rule 接口
│   │   ├── registry.go
│   │   ├── quotes.go
│   │   ├── indent.go
│   │   ├── pangu.go
│   │   ├── emphasis_space.go
│   │   ├── remove_separators.go
│   │   └── shift_headings.go
│   │
│   ├── llm/                          # LLM 处理引擎
│   │   ├── step.go                   # LLMStep
│   │   ├── action.go                 # rewrite / full_rewrite
│   │   ├── client.go                 # HTTP 客户端（重试、限流）
│   │   ├── provider.go               # Provider 接口
│   │   ├── providers/
│   │   │   ├── openai.go
│   │   │   ├── anthropic.go
│   │   │   └── ollama.go
│   │   ├── prompt/
│   │   │   ├── prompt.go
│   │   │   ├── registry.go
│   │   │   ├── loader.go
│   │   │   └── builtin/              # go:embed
│   │   └── json.go                   # JSON 解析与容错
│   │
│   ├── pipeline/                     # 处理管道
│   │   ├── pipeline.go
│   │   ├── builder.go                # 从配置构建 Pipeline
│   │   └── context.go
│   │
│   ├── config/                       # 配置管理
│   │   ├── config.go
│   │   ├── loader.go                 # 配置层叠加载
│   │   ├── defaults.go
│   │   └── frontmatter.go
│   │
│   └── output/                       # 输出格式化
│       ├── formatter.go              # Formatter 接口
│       ├── text.go                   # 默认文本输出
│       ├── json.go                   # --format json
│       └── diff.go                   # --diff 模式
│
└── internal/                         # 基础设施层（内部实现细节）
    ├── fileio/
    │   ├── reader.go                 # 统一的文件/stdin 读取
    │   └── writer.go                 # 原子写入（临时文件 + rename）
    ├── signal/
    │   └── handler.go                # 优雅退出与临时文件清理
    ├── xdg/
    │   └── dirs.go                   # XDG 基础目录规范
    └── version/
        └── version.go                # 版本信息（编译时注入）
```

### 2.2 核心抽象

**Document（中间表示）：**

```go
// pkg/core/document.go
type Document struct {
    Source  []byte    // 原始 Markdown 源文本
    AST     ast.Node  // goldmark 解析的 AST（只读，用于结构识别）
    Regions RegionMap // 字节区间 → 文档结构类型的映射
}

// 构建 Document 是进入处理管道的第一步
func Parse(source []byte) (*Document, error)
```

**RegionMap（结构识别）：**

```go
// pkg/core/region.go
type RegionType int

const (
    RegionText RegionType = iota
    RegionCode
    RegionHTML
    RegionHeading
    RegionListItem
    RegionTable
    RegionBlockquote
    RegionFrontMatter
    RegionEmphasisMarker
)

type Region struct {
    Start int
    End   int
    Type  RegionType
    Meta  map[string]any  // heading level, code language 等
}

type RegionMap struct {
    Regions []Region
}

func (rm *RegionMap) TypeAt(offset int) RegionType
func (rm *RegionMap) FilterByType(types ...RegionType) []Region
```

**AnnotatedDoc（LLM 输入）：**

```go
// pkg/core/annotated.go
type AnnotatedDoc struct {
    Original  []byte                     // 原始文本（不带标注）
    Annotated []byte                     // 带标注的文本（[P1], [H1] 等）
    Nodes     []AnnotatedNode
    NodeIndex map[string]*AnnotatedNode  // ID → Node
}

type AnnotatedNode struct {
    ID     string  // "P1", "H3"
    Type   string  // "paragraph", "heading"
    Start  int
    End    int
    Text   string
    Prefix string  // 前 15 个 UTF-8 字符（用于回贴校验）
    Level  int     // 仅 heading
}

func Annotate(doc *Document, nodeType string) (*AnnotatedDoc, error)
```

---

## 3. 入口层：CLI 设计

### 3.1 命令结构

```
dellm [flags] [file...]
dellm [command]

Commands:
  list-rules          列出所有可用的本地规则
  list-prompts        列出所有已加载的提示词
  version             显示版本信息

Flags:
  -c, --config string        配置文件路径
  -p, --profile string       使用的 profile（默认 "default"）
  -o, --output string        输出文件路径
  -i, --in-place             就地修改
  -d, --diff                 输出 unified diff
      --dry-run              报告将执行的操作，不实际处理
      --format string        输出格式: text|json（默认 text）
      --only strings         只运行指定规则/步骤（逗号分隔）
      --skip strings         跳过指定规则/步骤（逗号分隔）
      --skip-llm             跳过所有 LLM 步骤
      --shift-headings int   快捷方式：临时调整标题级别
      --stats                输出处理统计
      --no-color             禁用颜色输出
      --log-level string     日志级别: debug|info|warn|error（默认 info）
  -q, --quiet                静默模式（只输出错误）
  -v, --verbose              详细日志（= --log-level debug）
  -h, --help

Examples:
  dellm input.md                          # 输出到 stdout
  dellm input.md -o output.md
  dellm input.md -i                       # 就地修改
  cat input.md | dellm -                  # 从 stdin
  dellm *.md -i                           # 批量
  dellm input.md -p full --dry-run        # 预览
  dellm input.md --format json | jq .     # JSON 输出供管道使用
  dellm input.md --shift-headings=-1 -i   # 快捷调整标题
```

### 3.2 退出码约定

遵循 Unix sysexits.h 约定：

| 退出码 | 常量 | 含义 |
|--------|------|------|
| 0 | EX_OK | 成功 |
| 1 | EX_GENERAL | 一般性错误（文件读写失败、处理失败） |
| 2 | EX_USAGE | 命令行参数错误 |
| 70 | EX_SOFTWARE | 内部错误（断言失败、不应该到达的代码路径） |
| 78 | EX_CONFIG | 配置文件错误 |

```go
// internal/command/exitcode.go
const (
    ExitOK       = 0
    ExitGeneral  = 1
    ExitUsage    = 2
    ExitSoftware = 70
    ExitConfig   = 78
)
```

### 3.3 信号处理

```go
// internal/signal/handler.go
type Handler struct {
    cleanups []func()
    mu       sync.Mutex
}

func (h *Handler) Register(cleanup func()) {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.cleanups = append(h.cleanups, cleanup)
}

func (h *Handler) Setup() {
    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)
    go func() {
        <-c
        h.cleanup()
        // 重新发送信号，保证正确的退出状态
        signal.Reset(os.Interrupt, syscall.SIGTERM)
        syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
    }()
}

func (h *Handler) cleanup() {
    h.mu.Lock()
    defer h.mu.Unlock()
    for i := len(h.cleanups) - 1; i >= 0; i-- {
        h.cleanups[i]()
    }
}
```

在 main.go 中：

```go
func main() {
    sigHandler := signal.New()
    sigHandler.Setup()
  
    // 注册临时文件清理
    tmpMgr := fileio.NewTempManager()
    sigHandler.Register(tmpMgr.Cleanup)
  
    if err := cmd.Execute(); err != nil {
        os.Exit(command.ExitGeneral)
    }
}
```

---

## 4. 配置系统

### 4.1 配置层叠

从低到高（后者覆盖前者）：

```
1. 内置默认值（pkg/config/defaults.go）
2. 系统级配置（/etc/dellm/config.yaml）
3. 用户级配置（$XDG_CONFIG_HOME/dellm/config.yaml）
4. 项目级配置（./dellm.yaml 或 ./.dellm.yaml）
5. 文档级配置（Markdown front matter 中的 dellm: 块）
6. 环境变量（DELLM_* 前缀）
7. CLI flags
```

实现：

```go
// pkg/config/loader.go
type Loader struct {
    viper *viper.Viper
}

func (l *Loader) Load(cliFlags CLIFlags) (*Config, error) {
    // 1. 设置内置默认值
    l.setDefaults()
  
    // 2. 查找并加载配置文件（多层级合并）
    if err := l.loadConfigFiles(); err != nil {
        return nil, err
    }
  
    // 3. 环境变量覆盖
    l.viper.SetEnvPrefix("DELLM")
    l.viper.AutomaticEnv()
  
    // 4. CLI flags 覆盖
    l.applyFlags(cliFlags)
  
    cfg := &Config{}
    if err := l.viper.Unmarshal(cfg); err != nil {
        return nil, err
    }
  
    return cfg, cfg.Validate()
}

func (l *Loader) loadConfigFiles() error {
    paths := []string{
        "/etc/dellm/config.yaml",
        filepath.Join(xdg.ConfigHome(), "dellm", "config.yaml"),
        "./dellm.yaml",
        "./.dellm.yaml",
    }
  
    for _, path := range paths {
        if _, err := os.Stat(path); err == nil {
            l.viper.SetConfigFile(path)
            if err := l.viper.MergeInConfig(); err != nil {
                return fmt.Errorf("load %s: %w", path, err)
            }
        }
    }
    return nil
}
```

### 4.2 配置结构

```yaml
version: 1

profiles:
  default:
    preprocess:
      - remove_separators
    llm_steps: []
    postprocess:
      - quotes
      - indent
      - pangu
      - emphasis_space

  full:
    preprocess:
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
  quotes:
    style: curly
    single_quotes: true
    apostrophe_protection: true

  remove_separators:
    keep_frontmatter: true

  shift_headings:
    offset: 0
    scope: []
    on_overflow: clamp

  indent:
    char: "\u3000\u3000"
    scope: [paragraph]
    skip_first_paragraph: false

  pangu:
    space_char: " "

  emphasis_space:
    space_char: " "

llm:
  providers:
    default:
      type: openai
      base_url: "https://api.openai.com/v1"
      api_key: "${DELLM_API_KEY}"
      model: "gpt-4o"
      temperature: 0.3
      max_tokens: 4096
      timeout: 60s
      max_retries: 3

  prompts:
    dirs:
      - "./prompts"
      - "${XDG_CONFIG_HOME}/dellm/prompts"

  concurrency: 1
  rate_limit: "60/minute"
  on_failure: preserve

global:
  log_level: info
```

---

## 5. 规则系统

### 5.1 Rule 接口

```go
// pkg/rule/rule.go
type Rule interface {
    // Name 返回规则的唯一标识符
    Name() string
  
    // Description 返回一句话描述（用于 list-rules）
    Description() string
  
    // Transform 接收 Document，返回修改后的源文本
    // 如果不需要修改，返回 nil 和 nil（Pipeline 会保持原文）
    Transform(doc *core.Document) ([]byte, error)
}
```

### 5.2 规则注册表

```go
// pkg/rule/registry.go
type Factory func(config map[string]any) (Rule, error)

var registry = map[string]Factory{
    "quotes":              NewQuotesRule,
    "remove_separators":   NewRemoveSeparatorsRule,
    "shift_headings":      NewShiftHeadingsRule,
    "indent":              NewIndentRule,
    "pangu":               NewPanguRule,
    "emphasis_space":      NewEmphasisSpaceRule,
}

func Register(name string, factory Factory) {
    registry[name] = factory
}

func Get(name string, config map[string]any) (Rule, error) {
    factory, ok := registry[name]
    if !ok {
        return nil, fmt.Errorf("unknown rule: %s", name)
    }
    return factory(config)
}

func List() []RuleInfo {
    var infos []RuleInfo
    for name, factory := range registry {
        rule, _ := factory(nil)  // 用默认配置实例化来获取 Description
        infos = append(infos, RuleInfo{
            Name:        name,
            Description: rule.Description(),
        })
    }
    sort.Slice(infos, func(i, j int) bool {
        return infos[i].Name < infos[j].Name
    })
    return infos
}
```

### 5.3 具体规则（以 quotes 为例）

```go
// pkg/rule/quotes.go
type QuotesRule struct {
    style                string
    singleQuotes         bool
    apostropheProtection bool
}

func NewQuotesRule(cfg map[string]any) (Rule, error) {
    r := &QuotesRule{
        style:                getStringOr(cfg, "style", "curly"),
        singleQuotes:         getBoolOr(cfg, "single_quotes", true),
        apostropheProtection: getBoolOr(cfg, "apostrophe_protection", true),
    }
    return r, nil
}

func (r *QuotesRule) Name() string {
    return "quotes"
}

func (r *QuotesRule) Description() string {
    return "半角引号 → 中文弯引号（配对计数）"
}

func (r *QuotesRule) Transform(doc *core.Document) ([]byte, error) {
    // 只处理指定类型的 Region
    allowedTypes := []core.RegionType{
        core.RegionText,
        core.RegionHeading,
        core.RegionListItem,
        core.RegionTable,
        core.RegionBlockquote,
    }
  
    var result []byte
    lastEnd := 0
    doubleOpen := true
    singleOpen := true
  
    for _, region := range doc.Regions.FilterByType(allowedTypes...) {
        // 追加前一个 region 到当前 region 之间的原始内容
        result = append(result, doc.Source[lastEnd:region.Start]...)
      
        // 处理当前 region
        text := doc.Source[region.Start:region.End]
        processed := r.processText(text, &doubleOpen, &singleOpen)
        result = append(result, processed...)
      
        lastEnd = region.End
      
        // 段落边界重置状态
        if r.isParagraphBoundary(doc, region.End) {
            doubleOpen = true
            singleOpen = true
        }
    }
  
    // 追加剩余部分
    result = append(result, doc.Source[lastEnd:]...)
  
    return result, nil
}

func (r *QuotesRule) processText(text []byte, doubleOpen, singleOpen *bool) []byte {
    runes := []rune(string(text))
    var result []rune
  
    for i, ch := range runes {
        switch ch {
        case '"':
            if *doubleOpen {
                result = append(result, '"')
            } else {
                result = append(result, '"')
            }
            *doubleOpen = !*doubleOpen
      
        case '\'':
            if r.apostropheProtection && r.isApostrophe(runes, i) {
                result = append(result, ch)
            } else {
                if *singleOpen {
                    result = append(result, ''')
                } else {
                    result = append(result, ''')
                }
                *singleOpen = !*singleOpen
            }
      
        default:
            result = append(result, ch)
        }
    }
  
    return []byte(string(result))
}

func (r *QuotesRule) isApostrophe(runes []rune, pos int) bool {
    if pos == 0 || pos == len(runes)-1 {
        return false
    }
    prev := runes[pos-1]
    next := runes[pos+1]
    return isASCIILetter(prev) && isASCIILetter(next)
}
```

---

## 6. LLM 处理系统

### 6.1 LLMStep 结构

```go
// pkg/llm/step.go
type Step struct {
    Name     string
    Action   ActionType  // ActionRewrite | ActionFullRewrite
    Target   string      // paragraph | heading | list_item | blockquote
    Detect   bool
    Filter   *Filter
    Prompt   PromptRef
    Params   map[string]any
    Provider Provider
}

type ActionType int

const (
    ActionRewrite ActionType = iota
    ActionFullRewrite
)

type Filter struct {
    Level     []int   // 仅 heading
    MinLength int
    MaxLength int
}

type PromptRef struct {
    Detect  *prompt.Prompt  // nil if detect: false
    Rewrite *prompt.Prompt
}
```

### 6.2 rewrite Action 执行流程

```go
// pkg/llm/action.go
func (s *Step) executeRewrite(doc *core.Document) ([]byte, error) {
    // 1. 标注文档
    annotated, err := core.Annotate(doc, s.Target)
    if err != nil {
        return nil, err
    }
  
    // 2. 确定目标节点
    var targets []core.AnnotatedNode
  
    if s.Detect {
        // 2a. 检测轮
        targets, err = s.detectTargets(annotated)
        if err != nil {
            return nil, fmt.Errorf("detect: %w", err)
        }
    } else {
        // 2b. 程序 filter
        targets = s.filterNodes(annotated.Nodes)
    }
  
    if len(targets) == 0 {
        return doc.Source, nil  // 无需修改
    }
  
    // 3. 改写轮
    rewrites, err := s.rewriteTargets(annotated, targets)
    if err != nil {
        return nil, fmt.Errorf("rewrite: %w", err)
    }
  
    // 4. 回贴
    result, warnings := applyRewrites(annotated, rewrites)
    for _, w := range warnings {
        slog.Warn(w)
    }
  
    return result, nil
}

func (s *Step) detectTargets(annotated *core.AnnotatedDoc) ([]core.AnnotatedNode, error) {
    vars := map[string]any{
        "annotated_doc": string(annotated.Annotated),
    }
    mergeVars(vars, s.Params)
  
    promptText, err := s.Prompt.Detect.Render(vars)
    if err != nil {
        return nil, err
    }
  
    resp, err := s.Provider.Complete(context.Background(), &CompletionRequest{
        Messages:       []Message{{Role: "user", Content: promptText}},
        ResponseFormat: FormatJSON,
    })
    if err != nil {
        return nil, err
    }
  
    matches, err := parseDetectResponse(resp.Content)
    if err != nil {
        return nil, err
    }
  
    return validateMatches(annotated, matches), nil
}

func (s *Step) rewriteTargets(annotated *core.AnnotatedDoc, targets []core.AnnotatedNode) ([]Rewrite, error) {
    vars := map[string]any{
        "annotated_doc": string(annotated.Annotated),
        "target_nodes":  formatTargets(targets),
    }
    mergeVars(vars, s.Params)
  
    promptText, err := s.Prompt.Rewrite.Render(vars)
    if err != nil {
        return nil, err
    }
  
    resp, err := s.Provider.Complete(context.Background(), &CompletionRequest{
        Messages:       []Message{{Role: "user", Content: promptText}},
        ResponseFormat: FormatJSON,
    })
    if err != nil {
        return nil, err
    }
  
    return parseRewriteResponse(resp.Content)
}
```

### 6.3 JSON 解析与容错

```go
// pkg/llm/json.go
func parseRewriteResponse(content string) ([]Rewrite, error) {
    // 1. 尝试直接解析
    var result struct {
        Rewrites []Rewrite `json:"rewrites"`
    }
    if err := json.Unmarshal([]byte(content), &result); err == nil {
        return result.Rewrites, nil
    }
  
    // 2. 尝试剥离 Markdown 代码块
    if stripped := stripMarkdownCodeBlock(content); stripped != content {
        if err := json.Unmarshal([]byte(stripped), &result); err == nil {
            return result.Rewrites, nil
        }
    }
  
    // 3. 尝试修复常见问题（尾逗号）
    if fixed := fixTrailingCommas(content); fixed != content {
        if err := json.Unmarshal([]byte(fixed), &result); err == nil {
            return result.Rewrites, nil
        }
    }
  
    return nil, fmt.Errorf("failed to parse JSON: %w", err)
}

func stripMarkdownCodeBlock(s string) string {
    s = strings.TrimSpace(s)
    if strings.HasPrefix(s, "```json") {
        s = strings.TrimPrefix(s, "```json")
        s = strings.TrimPrefix(s, "```")
        s = strings.TrimSuffix(s, "```")
        s = strings.TrimSpace(s)
    }
    return s
}
```

### 6.4 回贴逻辑

```go
// pkg/llm/apply.go
type Rewrite struct {
    ID     string `json:"id"`
    Prefix string `json:"prefix"`
    Text   string `json:"text"`
}

func applyRewrites(annotated *core.AnnotatedDoc, rewrites []Rewrite) ([]byte, []string) {
    var warnings []string
  
    // 按节点在原文中的位置从后向前排序
    sort.Slice(rewrites, func(i, j int) bool {
        ni := annotated.NodeIndex[rewrites[i].ID]
        nj := annotated.NodeIndex[rewrites[j].ID]
        return ni.Start > nj.Start
    })
  
    result := append([]byte{}, annotated.Original...)
  
    for _, rw := range rewrites {
        node, ok := annotated.NodeIndex[rw.ID]
        if !ok {
            warnings = append(warnings, fmt.Sprintf("node %s not found", rw.ID))
            continue
        }
      
        // Prefix 校验
        if !strings.HasPrefix(node.Text, rw.Prefix) {
            warnings = append(warnings, fmt.Sprintf(
                "node %s prefix mismatch (expected '%s', got '%s')",
                rw.ID, rw.Prefix, node.Prefix,
            ))
            continue
        }
      
        // 替换
        result = append(
            result[:node.Start],
            append([]byte(rw.Text), result[node.End:]...)...,
        )
    }
  
    return result, warnings
}
```

---

## 7. Pipeline 编排

### 7.1 Pipeline 结构

```go
// pkg/pipeline/pipeline.go
type Pipeline struct {
    preprocess  []rule.Rule
    llmSteps    []*llm.Step
    postprocess []rule.Rule
    ctx         *Context
}

type Context struct {
    Stats       *Stats
    Logger      *slog.Logger
    DryRun      bool
    ShowProgress bool
}

type Stats struct {
    PreprocessChanges  map[string]int
    LLMTokensUsed      map[string]TokenUsage
    LLMCacheHits       int
    PostprocessChanges map[string]int
    Duration           time.Duration
}

type TokenUsage struct {
    Input    int
    Output   int
    Cached   int
}
```

### 7.2 执行主流程

```go
func (p *Pipeline) Process(source []byte) ([]byte, error) {
    start := time.Now()
    defer func() {
        p.ctx.Stats.Duration = time.Since(start)
    }()
  
    current := source
  
    // ① 预处理
    for _, r := range p.preprocess {
        p.ctx.Logger.Info("executing rule", "name", r.Name())
      
        doc, err := core.Parse(current)
        if err != nil {
            return nil, fmt.Errorf("parse: %w", err)
        }
      
        result, err := r.Transform(doc)
        if err != nil {
            return nil, fmt.Errorf("rule %s: %w", r.Name(), err)
        }
      
        if result != nil {
            changes := countChanges(current, result)
            p.ctx.Stats.PreprocessChanges[r.Name()] = changes
            current = result
        }
    }
  
    // ② LLM 步骤
    consecutiveFailures := 0
    for _, step := range p.llmSteps {
        p.ctx.Logger.Info("executing llm step", "name", step.Name)
      
        if p.ctx.DryRun {
            p.dryRunLLMStep(step, current)
            continue
        }
      
        doc, err := core.Parse(current)
        if err != nil {
            return nil, fmt.Errorf("parse: %w", err)
        }
      
        result, err := step.Execute(doc)
        if err != nil {
            consecutiveFailures++
            if consecutiveFailures >= 3 {
                return nil, fmt.Errorf("llm step %q: 连续3次失败", step.Name)
            }
            p.ctx.Logger.Warn("llm step failed", "name", step.Name, "err", err)
            continue
        }
      
        consecutiveFailures = 0
        current = result
    }
  
    // ③ 后处理
    for _, r := range p.postprocess {
        doc, err := core.Parse(current)
        if err != nil {
            return nil, fmt.Errorf("parse: %w", err)
        }
      
        result, err := r.Transform(doc)
        if err != nil {
            return nil, fmt.Errorf("rule %s: %w", r.Name(), err)
        }
      
        if result != nil {
            changes := countChanges(current, result)
            p.ctx.Stats.PostprocessChanges[r.Name()] = changes
            current = result
        }
    }
  
    return current, nil
}
```

### 7.3 从配置构建 Pipeline

```go
// pkg/pipeline/builder.go
type Builder struct {
    cfg           *config.Config
    ruleRegistry  *rule.Registry
    promptRegistry *prompt.Registry
}

func (b *Builder) Build(profileName string, cliOverrides CLIOverrides) (*Pipeline, error) {
    profile, ok := b.cfg.Profiles[profileName]
    if !ok {
        return nil, fmt.Errorf("profile %q not found", profileName)
    }
  
    // 应用 CLI 覆盖（--only, --skip, --skip-llm, --shift-headings）
    profile = b.applyOverrides(profile, cliOverrides)
  
    p := &Pipeline{
        ctx: &Context{
            Stats:  &Stats{},
            Logger: slog.Default(),
        },
    }
  
    // 构建预处理规则
    for _, name := range profile.Preprocess {
        ruleCfg := b.cfg.Rules[name]
        r, err := b.ruleRegistry.Get(name, ruleCfg)
        if err != nil {
            return nil, fmt.Errorf("preprocess rule %s: %w", name, err)
        }
        p.preprocess = append(p.preprocess, r)
    }
  
    // 构建 LLM 步骤
    for _, stepCfg := range profile.LLMSteps {
        step, err := b.buildLLMStep(stepCfg)
        if err != nil {
            return nil, fmt.Errorf("llm step %q: %w", stepCfg.Name, err)
        }
        p.llmSteps = append(p.llmSteps, step)
    }
  
    // 构建后处理规则
    for _, name := range profile.Postprocess {
        ruleCfg := b.cfg.Rules[name]
        r, err := b.ruleRegistry.Get(name, ruleCfg)
        if err != nil {
            return nil, fmt.Errorf("postprocess rule %s: %w", name, err)
        }
        p.postprocess = append(p.postprocess, r)
    }
  
    return p, nil
}

func (b *Builder) buildLLMStep(cfg LLMStepConfig) (*llm.Step, error) {
    // 加载 provider
    providerCfg := b.cfg.LLM.Providers[cfg.Provider]
    provider, err := llm.NewProvider(providerCfg)
    if err != nil {
        return nil, err
    }
  
    // 加载 prompt
    var detectPrompt, rewritePrompt *prompt.Prompt
    if cfg.Detect {
        detectPrompt, _ = b.promptRegistry.Get(cfg.Prompt.Detect)
        if detectPrompt == nil {
            return nil, fmt.Errorf("detect prompt %q not found", cfg.Prompt.Detect)
        }
    }
    rewritePrompt, _ = b.promptRegistry.Get(cfg.Prompt.Rewrite)
    if rewritePrompt == nil {
        return nil, fmt.Errorf("rewrite prompt %q not found", cfg.Prompt.Rewrite)
    }
  
    return &llm.Step{
        Name:     cfg.Name,
        Action:   parseAction(cfg.Action),
        Target:   cfg.Target,
        Detect:   cfg.Detect,
        Filter:   cfg.Filter,
        Prompt:   llm.PromptRef{Detect: detectPrompt, Rewrite: rewritePrompt},
        Params:   cfg.Params,
        Provider: provider,
    }, nil
}
```

---

## 8. 输出格式化

遵循"默认人类可读，提供机器可解析选项"的原则。

### 8.1 Formatter 接口

```go
// pkg/output/formatter.go
type Formatter interface {
    Format(result *Result) ([]byte, error)
}

type Result struct {
    Original []byte
    Modified []byte
    Stats    *pipeline.Stats
    Warnings []string
}

var formatters = map[string]Formatter{
    "text": &TextFormatter{},
    "json": &JSONFormatter{},
    "diff": &DiffFormatter{},
}

func Get(name string) (Formatter, error) {
    f, ok := formatters[name]
    if !ok {
        return nil, fmt.Errorf("unknown format: %s", name)
    }
    return f, nil
}
```

### 8.2 JSON 输出

```go
// pkg/output/json.go
type JSONFormatter struct{}

func (f *JSONFormatter) Format(result *Result) ([]byte, error) {
    output := map[string]any{
        "modified": string(result.Modified),
        "stats":    result.Stats,
        "warnings": result.Warnings,
    }
    return json.MarshalIndent(output, "", "  ")
}
```

使用：

```bash
dellm input.md --format json | jq '.stats.llm_tokens_used'
```

---

## 9. 测试策略

### 9.1 测试金字塔

```
          ╱╲
         ╱  ╲      端到端测试（cmd/dellm_test.go）
        ╱────╲     实际执行 CLI，验证输出和退出码
       ╱      ╲
      ╱        ╲   集成测试（pkg/pipeline/*_test.go）
     ╱──────────╲  测试 Pipeline + 多个 Rule 组合
    ╱            ╲
   ╱              ╲ 单元测试（pkg/rule/*_test.go）
  ╱────────────────╲ 测试单个 Rule 的 Transform 逻辑
```

### 9.2 Golden File 测试

```go
// pkg/rule/quotes_test.go
func TestQuotesRule(t *testing.T) {
    tests := []struct {
        name string
    }{
        {"basic"},
        {"nested"},
        {"code_block"},
        {"unclosed"},
    }
  
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            input := readGoldenFile(t, "quotes", tt.name+"_input.md")
            expected := readGoldenFile(t, "quotes", tt.name+"_expected.md")
          
            doc, err := core.Parse(input)
            require.NoError(t, err)
          
            rule, _ := NewQuotesRule(map[string]any{
                "style":                 "curly",
                "single_quotes":         true,
                "apostrophe_protection": true,
            })
          
            result, err := rule.Transform(doc)
            require.NoError(t, err)
            assert.Equal(t, string(expected), string(result))
        })
    }
}

func readGoldenFile(t *testing.T, category, name string) []byte {
    path := filepath.Join("testdata", category, name)
    data, err := os.ReadFile(path)
    require.NoError(t, err)
    return data
}
```

Golden file 位于：

```
pkg/rule/testdata/
├── quotes/
│   ├── basic_input.md
│   ├── basic_expected.md
│   ├── nested_input.md
│   ├── nested_expected.md
│   └── ...
├── indent/
├── pangu/
└── ...
```

### 9.3 LLM 步骤的 Mock 测试

```go
// pkg/llm/step_test.go
type MockProvider struct {
    responses []MockResponse
}

type MockResponse struct {
    MatchContent string
    Response     string
    Err          error
}

func (m *MockProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
    for _, r := range m.responses {
        if strings.Contains(req.Messages[0].Content, r.MatchContent) {
            if r.Err != nil {
                return nil, r.Err
            }
            return &CompletionResponse{Content: r.Response}, nil
        }
    }
    return nil, fmt.Errorf("no mock response")
}

func TestLLMStep_Rewrite(t *testing.T) {
    mockProvider := &MockProvider{
        responses: []MockResponse{
            {
                MatchContent: "annotated_doc",
                Response: `{
                    "rewrites": [
                        {"id": "P1", "prefix": "这不是一个问题", "text": "这是一个机会"}
                    ]
                }`,
            },
        },
    }
  
    // ... 测试逻辑
}
```

---

## 10. 性能优化

### 10.1 启动时间优化

- **延迟初始化**：只在执行 LLM 步骤时才加载 provider 和 prompt registry
- **并行加载配置文件**：对多个路径的配置文件并行读取（但合并仍然串行）
- **避免不必要的 goldmark 解析**：如果 `--dry-run` 且没有 LLM 步骤，跳过 AST 构建

```go
// cmd/main.go
func main() {
    // 快速路径：版本查询和帮助不需要加载配置
    if len(os.Args) == 2 {
        switch os.Args[1] {
        case "version", "--version", "-v":
            fmt.Println(version.Full())
            return
        case "help", "--help", "-h":
            cmd.Execute()
            return
        }
    }
  
    // 正常流程
    // ...
}
```

### 10.2 内存优化

- **流式处理**：逐文件处理，而非加载所有文件到内存
- **及时释放**：每个文件处理完后立即释放 Document 和 AnnotatedDoc

```go
// internal/command/process.go
func processFiles(files []string, pipeline *pipeline.Pipeline) error {
    for _, file := range files {
        if err := processFile(file, pipeline); err != nil {
            return err
        }
        // 隐式释放：函数返回后 GC 回收
    }
    return nil
}
```

### 10.3 进度反馈

```go
// pkg/pipeline/progress.go
type Progress struct {
    w       io.Writer
    total   int
    current int
    start   time.Time
}

func (p *Progress) Update(message string) {
    if !p.shouldShow() {
        return
    }
  
    elapsed := time.Since(p.start)
    percent := float64(p.current) / float64(p.total) * 100
    rate := float64(p.current) / elapsed.Seconds()
    eta := time.Duration(float64(p.total-p.current)/rate) * time.Second
  
    fmt.Fprintf(p.w, "\r%s: %d/%d (%.1f%%) | %.0f items/s | ETA: %s",
        message, p.current, p.total, percent, rate, eta.Truncate(time.Second))
}

func (p *Progress) shouldShow() bool {
    // 只在交互式终端显示进度
    f, ok := p.w.(*os.File)
    return ok && term.IsTerminal(int(f.Fd()))
}
```

---

## 11. 可扩展性设计

### 11.1 外部子命令

遵循 Git 的插件模式：任何名为 `dellm-<name>` 的可执行文件在 `$PATH` 中，就能通过 `dellm <name>` 调用。

```go
// cmd/root.go
func Execute() error {
    cmd := &cobra.Command{
        Use: "dellm",
        RunE: func(cmd *cobra.Command, args []string) error {
            // 如果第一个参数不是已知子命令，尝试外部命令
            if len(args) > 0 {
                extCmd := "dellm-" + args[0]
                if path, err := exec.LookPath(extCmd); err == nil {
                    c := exec.Command(path, args[1:]...)
                    c.Stdin = os.Stdin
                    c.Stdout = os.Stdout
                    c.Stderr = os.Stderr
                    return c.Run()
                }
            }
            return cmd.Help()
        },
    }
    // ...
    return cmd.Execute()
}
```

使用：

```bash
# 用户安装 dellm-stats 扩展（第三方工具）
$ go install github.com/someone/dellm-stats@latest

# 直接调用
$ dellm stats input.md
```

### 11.2 规则的热加载（未来）

预留接口，允许从外部加载规则：

```go
// pkg/rule/loader.go
type ExternalRule struct {
    path string
}

func LoadExternal(path string) (Rule, error) {
    // 使用 plugin.Open 加载 Go plugin
    // 或者通过 WASM 加载 WebAssembly 模块
    // 第一版不实现，预留接口
}
```

---

## 12. 部署与分发

### 12.1 构建

```bash
# Makefile
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -X github.com/yourorg/dellm/internal/version.Version=$(VERSION)

.PHONY: build
build:
	go build -ldflags "$(LDFLAGS)" -o dellm ./cmd

.PHONY: install
install:
	go install -ldflags "$(LDFLAGS)" ./cmd

.PHONY: test
test:
	go test -v ./...

.PHONY: lint
lint:
	golangci-lint run
```

### 12.2 发布

使用 GoReleaser：

```yaml
# .goreleaser.yaml
before:
  hooks:
    - go mod tidy

builds:
  - main: ./cmd
    binary: dellm
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X github.com/yourorg/dellm/internal/version.Version={{.Version}}

archives:
  - format: tar.gz
    name_template: "dellm_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: yourorg
    name: dellm
```

---

## 13. 内置提示词

内容与 v1.0 相同，位于 `pkg/llm/prompt/builtin/`：

- `detect_contrasting.md`
- `rewrite_contrasting.md`
- `rewrite_heading.md`
- `elaborate_steps.md`
- `compress.md`
- `polish.md`

---

## 14. 风险与限制

| 编号 | 项目 | 描述 | 应对 |
|------|------|------|------|
| R1 | goldmark AST 偏移精度 | 需验证 goldmark 节点的字节偏移精度 | 编码前写 spike 测试验证 |
| R2 | 单引号撇号启发式 | "前后都是字母"对 `'twas` 误判 | 可接受，极少见 |
| R3 | LLM 幻觉 ID | LLM 可能返回不存在的节点 ID | prefix 双重校验 + warning |
| R4 | LLM JSON 格式污染 | 可能返回 Markdown 包裹的 JSON | 解析容错：剥离代码块、修复尾逗号 |
| R5 | 启动时间 | Go 编译型语言启动快，但配置加载可能慢 | 延迟初始化、并行加载配置文件 |
| R6 | 幂等性 | 反复运行会反复改写 | 用户自觉；未来可加内容 hash 缓存 |
| R7 | 段落跨行的 prefix | 提取前 15 字符时忽略换行 | 统一处理：只取可见字符 |
| R8 | Setext 标题 shift | 语法限制只能表达 h1/h2 | 第一版不处理 Setext，warning |

---

## 15. 开发路线图

### Phase 1: 核心功能（v0.1）

- [x] 项目骨架与模块划分
- [ ] 6 个本地规则（quotes, indent, pangu, emphasis_space, remove_separators, shift_headings）
- [ ] Pipeline 编排
- [ ] 配置系统（YAML 加载、层叠合并）
- [ ] CLI 基础（cobra + flags）
- [ ] Golden file 测试

### Phase 2: LLM 集成（v0.2）

- [ ] Provider 接口与 OpenAI 实现
- [ ] Prompt 系统（registry + loader + 内置 6 个）
- [ ] LLM Step 执行（rewrite + full_rewrite）
- [ ] JSON 解析与容错
- [ ] 回贴逻辑与 prefix 校验
- [ ] --dry-run 的 token 预估

### Phase 3: 完善与优化（v0.3）

- [ ] Anthropic / Ollama provider
- [ ] 输出格式化（--format json, --diff）
- [ ] 信号处理与临时文件清理
- [ ] 进度反馈
- [ ] --stats 统计输出
- [ ] XDG 目录规范
- [ ] 端到端测试

### Phase 4: 扩展性（v1.0）

- [ ] Front matter 配置覆盖
- [ ] 外部子命令支持
- [ ] 更多内置提示词
- [ ] 完整文档（README + 使用指南）
- [ ] CI/CD（GitHub Actions + GoReleaser）

---

## 16. 附录：用户旅程示例

**场景：** 拿到 LLM 输出的中文技术文档，做完整清理。

**LLM 输出的问题：**
- 从 `##` 开始输出（应该从 `#` 开始）
- 有"不是A而是B"句式
- 标题含冒号/破折号
- 内容跳步骤
- 半角引号、无空格

**配置：**

```yaml
# ~/.config/dellm/config.yaml
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
```

**使用：**

```bash
export DEEPSEEK_API_KEY=xxx

# 预览
dellm article.md -p full --dry-run

# 执行
dellm article.md -p full -i --stats

# 输出统计
# ── 处理统计 ──────────────────────────
# 文件: article.md
# 
# 预处理:
#   shift_headings:      调整 8 个标题
#   remove_separators:   删除 3 处分隔线
# 
# LLM 步骤:
#   "全文细化":            成功
#     Token: 11,800 (输入) + 14,500 (输出)
#   "重写标题":            处理 8 个节点，成功 8
#     Token: 27,000 (输入) + 800 (输出), 缓存命中: 13,000
#   "去掉转折对比句式":     检测 5 个节点，改写成功 5
#     Token: 27,500 (输入) + 1,700 (输出), 缓存命中: 26,800
# 
# 后处理:
#   quotes:               替换 24 处引号
#   indent:               缩进 15 个段落
#   pangu:                插入 47 处空格
#   emphasis_space:       插入 8 处空格
# 
# 总 Token: 66,300 (输入) + 17,000 (输出)
# 缓存命中: 39,800 tokens (60.0%)
# 耗时: 52.3s (LLM: 51.1s, 本地: 1.2s)
# ──────────────────────────────────────
```

---

**设计文档结束。**

本设计文档遵循《CLI应用架构之禅》的原则，将 DeLLM 构建为一个架构清晰、职责分明、易于测试和扩展的命令行工具。核心思想是"CLI 作为库的壳"——业务逻辑封装在 `pkg/` 下的库包中，命令行层只是薄壳。这种架构使得核心逻辑可以独立测试、独立演进，甚至可以被其他程序（GUI、Web 服务）复用。