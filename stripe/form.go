package stripe

import (
	"fmt"
	"maps"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// EncodeForm flattens params into Stripe's bracketed form encoding, for
// example metadata[k]=v, items[0][price]=p and expand[0]=f.
//
// Nil values, nil pointers and empty maps or slices are omitted. An empty
// string is sent, which is how Stripe clears a field or a metadata key.
// Supported value types are string, bool, int, int64, float64, pointers to
// those, []string, []any, []map[string]any, map[string]string and
// map[string]any.
func EncodeForm(params map[string]any) (url.Values, error) {
	values := url.Values{}
	for _, key := range slices.Sorted(maps.Keys(params)) {
		if err := checkKey(key, key); err != nil {
			return nil, err
		}

		if err := encodeValue(values, key, params[key]); err != nil {
			return nil, err
		}
	}

	return values, nil
}

func encodeValue(values url.Values, key string, v any) error {
	switch v := v.(type) {
	case nil:
	case string:
		values.Set(key, v)
	case *string:
		if v != nil {
			values.Set(key, *v)
		}
	case bool:
		values.Set(key, strconv.FormatBool(v))
	case *bool:
		if v != nil {
			values.Set(key, strconv.FormatBool(*v))
		}
	case int:
		values.Set(key, strconv.Itoa(v))
	case *int:
		if v != nil {
			values.Set(key, strconv.Itoa(*v))
		}
	case int64:
		values.Set(key, strconv.FormatInt(v, 10))
	case *int64:
		if v != nil {
			values.Set(key, strconv.FormatInt(*v, 10))
		}
	case float64:
		return encodeFloat(values, key, v)
	case *float64:
		if v != nil {
			return encodeFloat(values, key, *v)
		}
	case []string:
		for i, item := range v {
			values.Set(indexKey(key, i), item)
		}
	case []any:
		for i, item := range v {
			if err := encodeValue(values, indexKey(key, i), item); err != nil {
				return err
			}
		}
	case []map[string]any:
		for i, item := range v {
			if err := encodeValue(values, indexKey(key, i), item); err != nil {
				return err
			}
		}
	case map[string]string:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if err := checkKey(key, k); err != nil {
				return err
			}

			values.Set(key+"["+k+"]", v[k])
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(v)) {
			if err := checkKey(key, k); err != nil {
				return err
			}

			if err := encodeValue(values, key+"["+k+"]", v[k]); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("stripe: cannot form-encode %s: unsupported type %T", key, v)
	}

	return nil
}

func encodeFloat(values url.Values, key string, f float64) error {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("stripe: cannot form-encode %s: %v is not a finite number", key, f)
	}

	values.Set(key, strconv.FormatFloat(f, 'f', -1, 64))
	return nil
}

// checkKey rejects keys that would collide with Stripe's bracket syntax.
func checkKey(parent, key string) error {
	if key == "" || strings.ContainsAny(key, "[]") {
		return fmt.Errorf("stripe: cannot form-encode %s: invalid key %q", parent, key)
	}

	return nil
}

func indexKey(key string, i int) string {
	return key + "[" + strconv.Itoa(i) + "]"
}
