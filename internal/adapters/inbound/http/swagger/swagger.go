// Package swagger mounts Swagger UI from swag-generated docs (github.com/swaggo/gin-swagger).
package swagger

import (
	"strings"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "github.com/turahe/blog-api/docs" // register swag-generated OpenAPI
)

const uiPath = "/swagger"

// ContentSecurityPolicy allows the embedded Swagger UI assets on /swagger/*.
const ContentSecurityPolicy = "default-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'"

// IsDocsPath reports whether the request is for Swagger UI or its generated spec.
func IsDocsPath(path string) bool {
	return path == uiPath || strings.HasPrefix(path, uiPath+"/")
}

// Mount registers Swagger UI at /swagger/*any.
func Mount(router gin.IRoutes) error {
	router.GET(uiPath+"/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	return nil
}
