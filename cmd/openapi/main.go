// Command openapi writes the API's OpenAPI document to stdout or a file.
// The document is generated from the Go handler types; see `make openapi`.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/lucasgoveia/stock-portfolio/internal/api"
)

func main() {
	out := flag.String("o", "", "output file (default stdout)")
	flag.Parse()
	b, err := json.MarshalIndent(api.OpenAPI(api.AllRoutes()...), "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	b = append(b, '\n')
	if *out == "" {
		_, _ = os.Stdout.Write(b)
		return
	}
	if err := os.WriteFile(*out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
