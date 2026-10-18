package state

import (
	"encoding/json"
	"fmt"
)

// LoadJSON reads the JSON file at path into a T.
//
// A missing file is Missing with a nil error. A file that cannot be read is Unreadable. A file
// whose content is not exactly one JSON value, with only whitespace around it, is Corrupt: an
// empty file, malformed JSON, trailing content after the value, and a value that does not fit T.
// On any status but Loaded the value is the zero T. A file holding null is Loaded as the zero T.
func LoadJSON[T any](path string) (T, LoadStatus, error) {
	return load(path, func(data []byte, v *T) error {
		return json.Unmarshal(data, v)
	})
}

// SaveJSON writes v to path as JSON, indented by two spaces and ending in a newline, with
// WriteFile at mode 0600. When v does not marshal, SaveJSON returns the error and leaves path
// untouched. The directory must exist.
func SaveJSON[T any](path string, v T) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("state: encode %s: %w", path, err)
	}

	return WriteFile(path, append(data, '\n'), 0o600)
}
