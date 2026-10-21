package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// List decodes out as JSON arrays of T or a stream of JSON objects, one T per object; blank input yields an empty slice.
func List[T any](out []byte) ([]T, error) {
	items := []T{}
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == io.EOF {
			return items, nil
		} else if err != nil {
			return nil, err
		}
		var err error
		switch raw[0] {
		case '[':
			var page []T
			err = json.Unmarshal(raw, &page)
			items = append(items, page...)
		case '{':
			var item T
			err = json.Unmarshal(raw, &item)
			items = append(items, item)
		default:
			err = fmt.Errorf("want a JSON object or array, got %.16s", raw)
		}
		if err != nil {
			return nil, err
		}
	}
}

// Names collects pick(item) for every item, skipping empty strings.
func Names[T any](items []T, pick func(T) string) []string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		if name := pick(it); name != "" {
			names = append(names, name)
		}
	}
	return names
}
