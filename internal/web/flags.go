package web

import (
	"io/fs"
	"net/http"
	"strings"
)

// Country codes come from the statistics dictionary. Only fixed, embedded
// two-letter SVG names are served; a request can never choose a disk path.
func flagCodes() []string {
	entries, _ := fs.ReadDir(assets, "static/flags")
	var codes []string
	for _, entry := range entries {
		name := entry.Name()
		if validFlagName(name) {
			codes = append(codes, strings.ToUpper(name[:2]))
		}
	}
	return codes
}

func validFlagName(name string) bool {
	return len(name) == 6 && name[2:] == ".svg" && name[0] >= 'a' && name[0] <= 'z' && name[1] >= 'a' && name[1] <= 'z'
}

func (s *server) flag(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/static/flags/")
	if !validFlagName(name) {
		http.NotFound(w, r)
		return
	}
	b, err := fs.ReadFile(assets, "static/flags/"+name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method != http.MethodHead {
		_, _ = w.Write(b)
	}
}
