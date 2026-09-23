// Package output formats pipeline results for humans and programs.
package output

import (
	"encoding/json"
	"fmt"

	"github.com/RobiNexy/de-llm/pkg/pipeline"
)

// Result is the stable boundary between document processing and presentation.
// Modified is always the processed document; Original is optional for callers
// that do not need a diff.
type Result struct {
	Original []byte
	Modified []byte
	Stats    map[string]map[string]int
	Warnings []string
}

// Formatter converts a processing result into an output document.
type Formatter interface {
	Format(*Result) ([]byte, error)
}

// TextFormatter emits only the processed Markdown.
type TextFormatter struct{}

func (TextFormatter) Format(result *Result) ([]byte, error) {
	return append([]byte(nil), result.Modified...), nil
}

// JSONFormatter emits a deterministic, machine-readable envelope.
type JSONFormatter struct{}

func (JSONFormatter) Format(result *Result) ([]byte, error) {
	warnings := result.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	stats := result.Stats
	if stats == nil {
		stats = map[string]map[string]int{}
	}
	return json.MarshalIndent(struct {
		Modified string                    `json:"modified"`
		Stats    map[string]map[string]int `json:"stats"`
		Warnings []string                  `json:"warnings"`
	}{string(result.Modified), stats, warnings}, "", "  ")
}

// Get returns a supported formatter.
func Get(name string) (Formatter, error) {
	switch name {
	case "text", "":
		return TextFormatter{}, nil
	case "json":
		return JSONFormatter{}, nil
	default:
		return nil, fmt.Errorf("unknown output format: %s", name)
	}
}

// Diff formats a result as a unified diff. It remains separate from Formatter
// because diff output needs a filename and is selected by --diff.
func Diff(result *Result, from, to string) []byte {
	return []byte(pipeline.UnifiedDiff(from, to, result.Original, result.Modified, 3))
}
