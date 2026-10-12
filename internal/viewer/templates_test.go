package viewer

import (
	"html/template"
	"testing"
)

func TestEmbeddedTemplates(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"viewer.html", "templates/viewer.html"},
		{"gallery.html", "templates/gallery.html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := template.New(tt.name).
				Funcs(template.FuncMap{
					"published": publicationDate,
				}).
				ParseFS(templates, tt.path)

			if err != nil {
				t.Fatalf("parse embedded template %q: %v", tt.path, err)
			}
		})
	}
}
