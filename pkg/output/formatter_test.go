package output

import (
	"encoding/json"
	"testing"
)

// The JSON contract must stay parseable and must represent absent collections
// as empty arrays/maps rather than JSON null, so shell consumers can iterate.
func TestJSONFormatterContract(t *testing.T) {
	data, err := (JSONFormatter{}).Format(&Result{Modified: []byte("# 标题\n")})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Modified string                    `json:"modified"`
		Stats    map[string]map[string]int `json:"stats"`
		Warnings []string                  `json:"warnings"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Modified != "# 标题\n" {
		t.Fatalf("modified = %q", got.Modified)
	}
	if got.Stats == nil || got.Warnings == nil {
		t.Fatalf("empty collections must not be null: %s", data)
	}
}

func TestGetRejectsUnknownFormat(t *testing.T) {
	if _, err := Get("yaml"); err == nil {
		t.Fatal("expected unknown format error")
	}
}
