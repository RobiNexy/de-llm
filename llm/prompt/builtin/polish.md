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
