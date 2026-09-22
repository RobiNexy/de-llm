package markdown

import (
	"strings"
	"testing"
)

// regionOf 返回包含目标子串的区域的类型。
func regionOf(t *testing.T, rm *RegionMap, substr string) RegionType {
	t.Helper()
	idx := strings.Index(string(rm.Source), substr)
	if idx < 0 {
		t.Fatalf("子串 %q 不存在", substr)
	}
	r, ok := rm.RegionAt(idx)
	if !ok {
		t.Fatalf("偏移 %d 未被任何区域覆盖", idx)
	}
	return r.Type
}

func TestRegionMapBasic(t *testing.T) {
	src := []byte("# Title\n\nHello `code` world\n\n```go\nfmt.Println(\"hi\")\n```\n\ntail 中文\n")
	rm, err := BuildRegionMap(src)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]RegionType{
		"Title":       RegionHeading,
		"Hello ":      RegionText,
		"code":        RegionCode,
		"fmt.Println": RegionCode,
		"tail 中文":     RegionText,
	}
	for substr, want := range cases {
		if got := regionOf(t, rm, substr); got != want {
			t.Errorf("%q → %v, 期望 %v", substr, got, want)
		}
	}
}

func TestRegionMapFencedCodeWholeBlock(t *testing.T) {
	// 围栏行与 info string 也应标记为代码。
	src := []byte("para\n\n```go\ncode body\n```\n\nafter\n")
	rm, _ := BuildRegionMap(src)
	for _, substr := range []string{"```go", "code body", "```"} {
		if got := regionOf(t, rm, substr); got != RegionCode {
			t.Errorf("%q → %v, 期望 code", substr, got)
		}
	}
}

func TestRegionMapFrontMatter(t *testing.T) {
	src := []byte("---\ntitle: 测试\n---\n\n# 标题\n\n正文\n")
	rm, _ := BuildRegionMap(src)
	if rm.FrontMatter == nil {
		t.Fatal("front matter 未识别")
	}
	if got := regionOf(t, rm, "title: 测试"); got != RegionFrontMatter {
		t.Errorf("front matter → %v", got)
	}
	if got := regionOf(t, rm, "标题"); got != RegionHeading {
		t.Errorf("标题 → %v", got)
	}
}

func TestRegionMapTable(t *testing.T) {
	src := []byte("| a | b |\n|---|---|\n| 1 | 2 |\n")
	rm, _ := BuildRegionMap(src)
	if got := regionOf(t, rm, "a"); got != RegionTable {
		t.Errorf("表格 → %v", got)
	}
}

func TestRegionMapListAndQuote(t *testing.T) {
	src := []byte("- 列表项一\n- 列表项二\n\n> 引用文本\n")
	rm, _ := BuildRegionMap(src)
	if got := regionOf(t, rm, "列表项一"); got != RegionListItem {
		t.Errorf("列表项 → %v", got)
	}
	if got := regionOf(t, rm, "引用文本"); got != RegionBlockquote {
		t.Errorf("引用块 → %v", got)
	}
}

func TestRegionMapEmphasisMarkers(t *testing.T) {
	src := []byte("前文***加粗斜体***后文**纯粗体**。\n")
	rm, _ := BuildRegionMap(src)
	if len(rm.EmphasisMarkers) == 0 {
		t.Fatal("无 emphasis marker 记录")
	}
	// 整个 "***" run 应为 marker 区域。
	idx := strings.Index(string(src), "***")
	r, _ := rm.RegionAt(idx)
	if r.Type != RegionEmphasisMarker {
		t.Errorf("起始 *** → %v", r.Type)
	}
	// 标记之间的"加粗斜体"是正文。
	if got := regionOf(t, rm, "加粗斜体"); got != RegionText {
		t.Errorf("强调内容 → %v", got)
	}
	if got := regionOf(t, rm, "纯粗体"); got != RegionText {
		t.Errorf("强调内容 → %v", got)
	}
}

func TestRegionMapRegionsNonOverlapping(t *testing.T) {
	src := []byte("> 引用**加粗**文本\n\n正文`代码`尾\n\n| 表 | 格 |\n|---|---|\n| a | b |\n")
	rm, _ := BuildRegionMap(src)
	for i := 1; i < len(rm.Regions); i++ {
		if rm.Regions[i].Start < rm.Regions[i-1].End {
			t.Fatalf("区域重叠: [%d,%d) 与 [%d,%d)",
				rm.Regions[i-1].Start, rm.Regions[i-1].End,
				rm.Regions[i].Start, rm.Regions[i].End)
		}
	}
	if len(rm.Regions) == 0 || rm.Regions[0].Start != 0 {
		t.Fatal("区域未从 0 开始")
	}
	last := rm.Regions[len(rm.Regions)-1]
	if last.End != len(src) {
		t.Fatalf("区域未覆盖到文尾: %d != %d", last.End, len(src))
	}
}

func TestAnnotateDocument(t *testing.T) {
	src := []byte("# 主标题\n\n第一段内容。\n\n## 副标题：一个冗长的标题\n\n第二段内容。\n\n- 列表项\n\n> 引用\n")
	doc := AnnotateDocument(src, "paragraph")
	if len(doc.Nodes) != 2 {
		t.Fatalf("段落标注数 %d != 2", len(doc.Nodes))
	}
	if doc.Nodes[0].ID != "P1" || doc.Nodes[1].ID != "P2" {
		t.Fatalf("ID 错误: %v", doc.Nodes)
	}
	if doc.Nodes[0].Prefix != "第一段内容。" {
		t.Errorf("Prefix = %q", doc.Nodes[0].Prefix)
	}
	// 标注文本含 [Pn] 前缀，且原偏移不变。
	if !strings.Contains(string(doc.Annotated), "[P1] 第一段内容。") {
		t.Errorf("标注渲染错误: %q", doc.Annotated)
	}
	if !strings.Contains(string(doc.Annotated), "[P2] 第二段内容。") {
		t.Errorf("标注渲染错误: %q", doc.Annotated)
	}
}

func TestAnnotateHeadings(t *testing.T) {
	src := []byte("# 一级\n\n## 二级\n\n正文\n")
	doc := AnnotateDocument(src, "heading")
	if len(doc.Nodes) != 2 {
		t.Fatalf("标题标注数 %d != 2", len(doc.Nodes))
	}
	// 标题节点范围含 # 标记。
	if doc.Nodes[0].Text != "# 一级" {
		t.Errorf("标题 Text = %q", doc.Nodes[0].Text)
	}
	if doc.Nodes[0].Level != 1 || doc.Nodes[1].Level != 2 {
		t.Errorf("Level 错误: %d %d", doc.Nodes[0].Level, doc.Nodes[1].Level)
	}
}

func TestVisiblePrefixSkipsNewline(t *testing.T) {
	// R9：段落含软换行时 prefix 只取可见字符。
	got := visiblePrefix("第一行\n第二行内容", 5)
	if strings.Contains(got, "\n") {
		t.Errorf("prefix 不应包含换行: %q", got)
	}
	if got != "第一行第二" {
		t.Errorf("prefix = %q", got)
	}
}

func TestFormatTargets(t *testing.T) {
	src := []byte("第一段。\n\n第二段。\n")
	doc := AnnotateDocument(src, "paragraph")
	ptrs := make([]*AnnotatedNode, len(doc.Nodes))
	for i := range doc.Nodes {
		ptrs[i] = &doc.Nodes[i]
	}
	out := FormatTargets(ptrs)
	if !strings.Contains(out, "[P1] 第一段。") {
		t.Errorf("target_nodes = %q", out)
	}
}
