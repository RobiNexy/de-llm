package core

import "testing"

func TestParseExposesRegions(t *testing.T) {
	doc, err := Parse([]byte("# 标题\n\n正文 `code`\n"))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Regions.TypeAt(2) != RegionHeading {
		t.Fatalf("offset 2 type = %v", doc.Regions.TypeAt(2))
	}
	if len(doc.Regions.FilterByType(RegionCode)) == 0 {
		t.Fatal("expected code region")
	}
}

func TestAnnotateRejectsNilDocument(t *testing.T) {
	if _, err := Annotate(nil, "paragraph"); err != ErrNilDocument {
		t.Fatalf("error = %v", err)
	}
}
