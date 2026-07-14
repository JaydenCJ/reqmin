// Command reqmin shrinks an HTTP request to the minimal set of headers and
// parameters that still reproduce a behavior, using delta debugging.
package main

import (
	"os"

	"github.com/JaydenCJ/reqmin/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, nil))
}
