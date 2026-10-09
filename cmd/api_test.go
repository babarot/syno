package cmd

import (
	"net/url"
	"reflect"
	"testing"
)

func TestParseFields(t *testing.T) {
	got, err := parseFields([]string{"name=homes", "additional=[\"a\",\"b\"]", "q=a=b", "name=public"})
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"name":       {"homes", "public"},
		"additional": {`["a","b"]`},
		"q":          {"a=b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	for _, bad := range []string{"noequals", "=value"} {
		if _, err := parseFields([]string{bad}); err == nil {
			t.Errorf("parseFields(%q) should fail", bad)
		}
	}
}

func TestJSONEncodeValues(t *testing.T) {
	params := url.Values{
		"name":       {"homes"},
		"quoted":     {`"homes"`},
		"additional": {`["share_quota"]`},
		"limit":      {"10"},
		"enabled":    {"true"},
	}
	jsonEncodeValues(params)
	want := url.Values{
		"name":       {`"homes"`},
		"quoted":     {`"homes"`},
		"additional": {`["share_quota"]`},
		"limit":      {"10"},
		"enabled":    {"true"},
	}
	if !reflect.DeepEqual(params, want) {
		t.Errorf("got %v, want %v", params, want)
	}
}

func TestIsReadMethod(t *testing.T) {
	tests := map[string]bool{
		"list":           true,
		"get":            true,
		"info":           true,
		"load_info":      true,
		"query":          true,
		"status":         true,
		"get_config":     true,
		"list_share":     true,
		"load_all":       true,
		"set":            false,
		"delete":         false,
		"shutdown":       false,
		"reboot":         false,
		"export":         false,
		"get_":           false,
		"getall":         false,
		"List":           false,
		"set_get_config": false,
		"":               false,
	}
	for method, want := range tests {
		if got := isReadMethod(method); got != want {
			t.Errorf("isReadMethod(%q) = %v, want %v", method, got, want)
		}
	}
}
