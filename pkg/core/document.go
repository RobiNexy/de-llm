// Package core exposes the stable document model used by processing clients.
// The implementation is backed by the existing markdown package so the
// command and external callers observe the same AST offsets and regions.
package core

import (
	"github.com/RobiNexy/de-llm/pkg/parser"
	"github.com/yuin/goldmark/ast"
)

// Document is an immutable parse result for one Markdown source.
// Rules must return new source bytes; AST offsets are valid only for Source.
type Document struct {
	Source  []byte
	AST     ast.Node
	Regions *RegionMap
}

// Parse builds the AST and region map for source.
func Parse(source []byte) (*Document, error) {
	regions, err := parser.BuildRegionMap(source)
	if err != nil {
		return nil, err
	}
	return &Document{Source: source, AST: parser.Parse(source), Regions: regions}, nil
}

// RegionMap and Region are aliases to keep the core contract and the current
// rule implementation interoperable without copying byte-offset metadata.
type RegionMap = parser.RegionMap
type Region = parser.Region
type RegionType = parser.RegionType

const (
	RegionText           = parser.RegionText
	RegionCode           = parser.RegionCode
	RegionHTML           = parser.RegionHTML
	RegionHeading        = parser.RegionHeading
	RegionListItem       = parser.RegionListItem
	RegionTable          = parser.RegionTable
	RegionBlockquote     = parser.RegionBlockquote
	RegionFrontMatter    = parser.RegionFrontMatter
	RegionEmphasisMarker = parser.RegionEmphasisMarker
)
