package dsm

import (
	"encoding/json"
	"testing"
)

func TestNum(t *testing.T) {
	var v struct {
		A Num `json:"a"`
		B Num `json:"b"`
		C Num `json:"c"`
		D Num `json:"d"`
	}
	if err := json.Unmarshal([]byte(`{"a":42,"b":"3999969443840","c":"","d":"n/a"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.A != 42 || v.B != 3999969443840 || v.C != 0 || v.D != 0 {
		t.Errorf("got %+v", v)
	}
}
