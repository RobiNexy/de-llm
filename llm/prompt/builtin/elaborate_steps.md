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
