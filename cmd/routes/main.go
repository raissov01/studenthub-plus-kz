// routes prints the security audit from the server's actual registrations.
package main

import (
	"fmt"
	"github.com/raissov01/studenthub-plus-kz/pkg/server"
	"os"
)

func main() {
	doc, err := server.SecurityDocument()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Print(doc)
}
