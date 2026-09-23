// Package swagger serves Swagger UI backed by the published OpenAPI contract in contracts/.
package swagger

import (
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/turahe/blog-api/contracts"
)

const (
	specPath    = "/openapi.yaml"
	uiPath      = "/swagger"
	uiIndex     = "/swagger/index.html"
	contentYAML = "application/yaml; charset=utf-8"
)

// CSP allows Swagger UI assets from unpkg while keeping the rest of the API locked down.
const ContentSecurityPolicy = "default-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"script-src 'self' 'unsafe-inline' https://unpkg.com; " +
	"img-src 'self' data: https://unpkg.com; " +
	"font-src 'self' https://unpkg.com data:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'"

// IsDocsPath reports whether the request is for Swagger UI or the OpenAPI document.
func IsDocsPath(path string) bool {
	return path == specPath || path == uiPath || path == uiIndex || strings.HasPrefix(path, uiPath+"/")
}

// Mount registers OpenAPI YAML and Swagger UI routes on the engine.
func Mount(router gin.IRoutes) error {
	spec := contracts.OpenAPIBundle
	index := contracts.SwaggerIndex
	if len(spec) == 0 {
		return errMissing("openapi.bundle.yaml")
	}
	if len(index) == 0 {
		return errMissing("swagger/index.html")
	}

	router.GET(specPath, func(c *gin.Context) {
		c.Data(nethttp.StatusOK, contentYAML, spec)
	})
	router.GET(uiPath, func(c *gin.Context) {
		c.Redirect(nethttp.StatusFound, uiIndex)
	})
	router.GET(uiPath+"/", func(c *gin.Context) {
		c.Redirect(nethttp.StatusFound, uiIndex)
	})
	router.GET(uiIndex, func(c *gin.Context) {
		c.Data(nethttp.StatusOK, "text/html; charset=utf-8", index)
	})
	return nil
}

type missingAssetError string

func (e missingAssetError) Error() string { return "contracts: missing embedded asset " + string(e) }

func errMissing(name string) error { return missingAssetError(name) }
