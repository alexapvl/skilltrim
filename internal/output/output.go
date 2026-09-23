package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/alpkeskin/gotoon"
)

type Error struct {
	Error string   `json:"error"`
	Help  []string `json:"help,omitempty"`
}

func Write(writer io.Writer, value any, toon bool) error {
	if toon {
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("normalize TOON value: %w", err)
		}
		var normalized any
		if err := json.Unmarshal(data, &normalized); err != nil {
			return fmt.Errorf("normalize TOON value: %w", err)
		}
		encoded, err := gotoon.Encode(normalized)
		if err != nil {
			return fmt.Errorf("encode TOON: %w", err)
		}
		_, err = fmt.Fprintln(writer, encoded)
		return err
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
