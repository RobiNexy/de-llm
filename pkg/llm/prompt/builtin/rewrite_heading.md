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
