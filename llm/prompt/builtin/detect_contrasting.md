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
