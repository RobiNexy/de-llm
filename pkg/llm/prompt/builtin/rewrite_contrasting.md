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
