// Package contracts embeds the published OpenAPI document and Swagger UI assets.
package contracts

import (
	_ "embed"
)

// OpenAPIBundle is the committed OpenAPI 3.1 bundle served at /openapi.yaml.
//
//go:embed openapi.bundle.yaml
var OpenAPIBundle []byte

// SwaggerIndex is the Swagger UI HTML page served at /swagger/index.html.
//
//go:embed swagger/index.html
var SwaggerIndex []byte
