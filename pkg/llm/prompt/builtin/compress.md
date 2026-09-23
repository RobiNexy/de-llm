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
