package core

import "github.com/RobiNexy/de-llm/pkg/parser"

// AnnotatedDoc is the LLM-facing representation with stable source offsets.
type AnnotatedDoc = parser.AnnotatedDoc

// AnnotatedNode is a replaceable Markdown node.
type AnnotatedNode = parser.AnnotatedNode

// Annotate labels nodes of nodeType for an LLM prompt. Labels never enter the
// returned source and can therefore be safely removed after the LLM round.
func Annotate(doc *Document, nodeType string) (*AnnotatedDoc, error) {
	if doc == nil {
		return nil, ErrNilDocument
	}
	return parser.AnnotateDocument(doc.Source, nodeType), nil
}
