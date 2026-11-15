package server

import (
	"encoding/json"
	"regexp"
)

func jsonIndent(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

func regexpMust(p string) *regexp.Regexp { return regexp.MustCompile(p) }
