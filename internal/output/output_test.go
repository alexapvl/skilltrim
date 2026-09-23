package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestTOONUsesJSONSchema(t *testing.T) {
	value := struct {
		Name  string `json:"name"`
		Empty string `json:"empty,omitempty"`
	}{Name: "alpha"}
	var buffer bytes.Buffer
	if err := Write(&buffer, value, true); err != nil {
		t.Fatal(err)
	}
	got := buffer.String()
	if !strings.Contains(got, "name: alpha") || strings.Contains(got, "omitempty") || strings.Contains(got, "empty") {
		t.Fatalf("TOON = %q", got)
	}
}
