package main

import (
	"io/fs"
	"testing/fstest"
)

func testFS() fs.FS { return fstest.MapFS{"index.html": {Data: []byte("x")}} }
