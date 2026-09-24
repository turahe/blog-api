package main

import (
	"fmt"
	"os"

	"github.com/turahe/blog-api/cmd"
)

//	@title			Blog API
//	@version		1.0
//	@description	Hexagonal blog platform REST API. Envelope responses use { ok, code, data, meta, error }.
//	@host			localhost:8080
//	@BasePath		/
//	@schemes		http https

//	@contact.name	API Support
//	@contact.url	https://github.com/turahe/blog-api

//	@license.name	MIT
//	@license.url	https://opensource.org/licenses/MIT

// @securityDefinitions.apikey	Bearer
// @in							header
// @name						Authorization
// @description				Type "Bearer" followed by a space and the JWT access token.
func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
