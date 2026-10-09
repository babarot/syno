package dsm

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// Num is a number that DSM may encode either as a JSON number or as a
// string ("1234"). Sizes in SYNO.Storage.CGI.Storage are strings, for example.
type Num float64

func (n *Num) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*n = 0
		return nil
	}
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		// Leave unparsable values as zero rather than failing the whole call.
		*n = 0
		return nil
	}
	*n = Num(f)
	return nil
}

var _ json.Unmarshaler = (*Num)(nil)
