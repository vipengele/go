package config

import (
	"reflect"
	"strconv"
	"strings"
	"time"
)

// Source is the effective value of one key of a loaded configuration and where it came from.
type Source struct {
	// Key is the canonical key, such as "server.max_retries".
	Key string
	// Value is the bound value as text that Load converts back to the same value: a time.Duration
	// as time.Duration.String writes it ("1m30s"), a float in the shortest form that reads back
	// exactly, and a slice as its elements joined by ",". A nil slice and an empty one are both "",
	// which reads back as an empty slice, and an element holding a "," reads back as more than one
	// element.
	Value string
	// Origin is where the value was read, as the provider that set it names it, such as
	// "config.yaml:12" or "env APP_SERVER_MAX_RETRIES", or "default" when no provider set the key.
	Origin string
}

// formatValue returns the text of v, a value of a type a Schema accepts, as Source.Value describes.
func formatValue(v reflect.Value) string {
	if v.Kind() == reflect.Slice {
		items := make([]string, v.Len())
		for i := range items {
			items[i] = formatValue(v.Index(i))
		}
		return strings.Join(items, ",")
	}
	if v.Type() == reflect.TypeFor[time.Duration]() {
		return time.Duration(v.Int()).String()
	}
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(v.Uint(), 10)
	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(v.Float(), 'g', -1, v.Type().Bits())
	default:
		return v.String()
	}
}
