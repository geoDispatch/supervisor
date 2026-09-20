package httpapi

import (
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"
)

// registerSwagger mounts the Swagger UI at GET /swagger/.
// Open http://localhost:8080/swagger/ in a browser to explore the API.
func registerSwagger(mux *http.ServeMux) {
	mux.Handle("/swagger/", httpSwagger.WrapHandler)
}