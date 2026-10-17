package state

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// LoadYAML reads the YAML file at path into a T.
//
// A missing file is Missing with a nil error. A file that cannot be read is Unreadable. A file
// whose content is not exactly one YAML document is Corrupt: an empty file, a file holding only
// whitespace or comments, malformed YAML, a second document, and a value that does not fit T. On
// any status but Loaded the value is the zero T.
func LoadYAML[T any](path string) (T, LoadStatus, error) {
	return load(path, decodeYAML[T])
}

func decodeYAML[T any](data []byte, v *T) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))

	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("the file holds no YAML document")
		}

		return err
	}

	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}

		return fmt.Errorf("line %d: a second YAML document; the file holds one", extra.Line)
	}

	return nil
}

// SaveYAML writes v to path as YAML with WriteFile at mode 0600. When v does not marshal, SaveYAML
// returns the error and leaves path untouched. The YAML encoder panics on some types it cannot
// encode, such as a channel or a function; SaveYAML returns that panic as an error. The directory
// must exist.
func SaveYAML[T any](path string, v T) error {
	data, err := marshalYAML(v)
	if err != nil {
		return fmt.Errorf("state: encode %s: %w", path, err)
	}

	return WriteFile(path, data, 0o600)
}

func marshalYAML(v any) (data []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			data, err = nil, fmt.Errorf("%v", r)
		}
	}()

	return yaml.Marshal(v)
}
