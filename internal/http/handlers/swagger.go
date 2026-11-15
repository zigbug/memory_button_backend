package handlers

import (
	"net/http"
	"os"
)

// SwaggerHandler serves OpenAPI spec and Swagger UI.
type SwaggerHandler struct {
	specPath string
	title    string
}

// NewSwaggerHandler creates a handler serving the OpenAPI spec from specPath.
func NewSwaggerHandler(specPath, title string) *SwaggerHandler {
	if title == "" {
		title = "API — Swagger UI"
	}
	return &SwaggerHandler{specPath: specPath, title: title}
}

// ServeSpec serves the OpenAPI YAML file.
func (h *SwaggerHandler) ServeSpec(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(h.specPath)
	if err != nil {
		http.Error(w, "openapi spec not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_, _ = w.Write(data)
}

// ServeUI serves Swagger UI HTML pointing to the spec.
func (h *SwaggerHandler) ServeUI(w http.ResponseWriter, r *http.Request) {
	specURL := "/openapi.yaml"
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(swaggerUIHTML(specURL, h.title)))
}

func swaggerUIHTML(specURL, title string) string {
	return `<!DOCTYPE html>
<html lang="ru">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>` + title + `</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  <style>html{box-sizing:border-box;overflow-y:scroll}*,*:before,*:after{box-sizing:inherit}body{margin:0;background:#fafafa}</style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = () => {
      window.ui = SwaggerUIBundle({
        url: '` + specURL + `',
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
        layout: 'BaseLayout',
        persistAuthorization: true,
        displayRequestDuration: true,
        tryItOutEnabled: true,
      });
    };
  </script>
</body>
</html>`
}
