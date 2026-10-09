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
