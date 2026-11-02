package stripe

import (
	"math"
	"strings"
	"testing"
)

func TestEncodeForm(t *testing.T) {
	yes := true
	no := false
	name := "Dummy Co"
	amount := int64(500)
	var nilString *string
	var nilMap map[string]string

	tests := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"nil params", nil, ""},
		{"scalars", map[string]any{"amount": 1200, "currency": "usd", "percent": 12.5, "big": int64(1) << 40}, "amount=1200&big=1099511627776&currency=usd&percent=12.5"},
		{"booleans", map[string]any{"a": true, "b": false, "c": &yes, "d": &no}, "a=true&b=false&c=true&d=false"},
		{"pointers", map[string]any{"name": &name, "amount": &amount}, "amount=500&name=Dummy+Co"},
		{"nil fields omitted", map[string]any{"a": nil, "b": nilString, "c": (*bool)(nil), "d": (*int64)(nil), "e": "x"}, "e=x"},
		{"empty string is sent", map[string]any{"description": ""}, "description="},
		{"metadata", map[string]any{"metadata": map[string]string{"plan": "pro", "old": ""}}, "metadata%5Bold%5D=&metadata%5Bplan%5D=pro"},
		{"nil metadata sends nothing", map[string]any{"metadata": nilMap}, ""},
		{"empty metadata sends nothing", map[string]any{"metadata": map[string]string{}}, ""},
		{"expand", map[string]any{"expand": []string{"data.customer", "data.invoice"}}, "expand%5B0%5D=data.customer&expand%5B1%5D=data.invoice"},
		{
			"nested items",
			map[string]any{"items": []map[string]any{{"price": "price_dummy1", "quantity": 2}, {"price": "price_dummy2"}}},
			"items%5B0%5D%5Bprice%5D=price_dummy1&items%5B0%5D%5Bquantity%5D=2&items%5B1%5D%5Bprice%5D=price_dummy2",
		},
		{
			"json-shaped values",
			map[string]any{"created": map[string]any{"gte": float64(1700000000)}, "types": []any{"a", map[string]any{"b": true}}},
			"created%5Bgte%5D=1700000000&types%5B0%5D=a&types%5B1%5D%5Bb%5D=true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := EncodeForm(tt.params)
			if err != nil {
				t.Fatalf("EncodeForm: %v", err)
			}

			if enc := got.Encode(); enc != tt.want {
				t.Errorf("got %q, want %q", enc, tt.want)
			}
		})
	}
}

func TestEncodeFormDeterministic(t *testing.T) {
	params := map[string]any{"metadata": map[string]string{"z": "1", "a": "2", "m": "3"}, "b": "x", "a": "y"}
	first, err := EncodeForm(params)
	if err != nil {
		t.Fatal(err)
	}

	for range 50 {
		again, _ := EncodeForm(params)
		if again.Encode() != first.Encode() {
			t.Fatalf("encoding changed: %q vs %q", again.Encode(), first.Encode())
		}
	}
}

func TestEncodeFormErrors(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"unsupported type", map[string]any{"when": struct{}{}}, "when: unsupported type"},
		{"nested unsupported type", map[string]any{"items": []any{map[string]any{"x": []int{1}}}}, "items[0][x]: unsupported type"},
		{"NaN", map[string]any{"amount": math.NaN()}, "not a finite number"},
		{"Inf", map[string]any{"amount": math.Inf(1)}, "not a finite number"},
		{"bracket in top-level key", map[string]any{"expand[0]": "x"}, "invalid key"},
		{"bracket in metadata key", map[string]any{"metadata": map[string]string{"a]": "x"}}, "invalid key"},
		{"empty nested key", map[string]any{"created": map[string]any{"": 1}}, "invalid key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeForm(tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}
