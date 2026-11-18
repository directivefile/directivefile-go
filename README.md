# directivefile-go

A Go parser for directivefile.

## Install

```bash
go get github.com/directivefile/directivefile-go
```

## Parse a file

```go
package main

import (
	"fmt"
	"log"

	"github.com/directivefile/directivefile-go"
)

func main() {
	file, err := directivefile.Parse("config.df")
	if err != nil {
		log.Fatal(err)
	}

	for _, d := range file.Directives {
		// ...
	}
}
```

`Parse` resolves templates, includes, and imports. The returned `File` contains
only ordinary directives.d