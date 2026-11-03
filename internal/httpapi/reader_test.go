package httpapi_test

import (
	"io"
	"strings"
)

func newReader(body string) io.Reader { return strings.NewReader(body) }
