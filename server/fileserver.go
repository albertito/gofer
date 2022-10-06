package server

import (
	"html"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

// FileServer implements an equivalent of http.FileServer, but with custom
// directory listing.
func FileServer(root http.FileSystem) http.Handler {
	return &fileServer{
		root:  root,
		upsrv: http.FileServer(root),
	}
}

type fileServer struct {
	root  http.FileSystem
	upsrv http.Handler
}

func (fsrv *fileServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Ensure the URL path begins with a /.
	{
		upath := req.URL.Path
		if !strings.HasPrefix(upath, "/") {
			upath = "/" + upath
			req.URL.Path = upath
		}
	}

	// Redirect x/index.html to x/
	const indexhtml = "/index.html"
	if strings.HasSuffix(req.URL.Path, indexhtml) {
		localRedirect(w, req, "./")
		return
	}

	// Clean the path up. Removes the .., and it will end in a slash only if
	// it is the root.
	cleanPath := path.Clean(req.URL.Path)

	// Open and stat the path.
	f, err := fsrv.root.Open(cleanPath)
	if err != nil {
		toHTTPError(w, err)
		return
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		toHTTPError(w, err)
		return
	}

	// Redirect directories to ending with "/".
	if fi.IsDir() && !strings.HasSuffix(req.URL.Path, "/") {
		localRedirect(w, req, path.Base(req.URL.Path)+"/")
		return
	}

	// Strip "/" from regular files.
	if !fi.IsDir() && strings.HasSuffix(req.URL.Path, "/") {
		localRedirect(w, req, "../"+path.Base(req.URL.Path))
		return
	}

	// Serve the directory.
	if fi.IsDir() {
		// Try to serve from index.html first.
		idxPath := path.Join(cleanPath, indexhtml)
		idxf, err := fsrv.root.Open(idxPath)
		if err == nil {
			defer idxf.Close()

			idxfi, err := idxf.Stat()
			if err == nil {
				http.ServeContent(w, req, idxPath, idxfi.ModTime(), idxf)
				return
			}
		}

		// Fall back to listing.
		dirList(w, req, f)
		return
	}

	// Serve the file.
	http.ServeContent(w, req, cleanPath, fi.ModTime(), f)
}

// Local redirect, which keeps relative paths.
func localRedirect(w http.ResponseWriter, req *http.Request, dst string) {
	u := url.URL{
		Path:     dst,
		RawQuery: req.URL.RawQuery,
	}

	w.Header().Set("Location", u.String())
	w.WriteHeader(http.StatusMovedPermanently)
}

func toHTTPError(w http.ResponseWriter, err error) {
	if os.IsNotExist(err) {
		http.Error(w, "404 Not found", http.StatusNotFound)
		return
	}
	if os.IsPermission(err) {
		http.Error(w, "403 Forbidden", http.StatusForbidden)
		return
	}

	http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
	return
}

func dirList(w http.ResponseWriter, req *http.Request, f http.File) {
	dirs, err := f.Readdir(-1)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}

	// Sort the entries by name.
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	data := struct {
		Dirs []os.FileInfo
	}{
		Dirs: dirs,
	}
	dirListTmpl.Execute(w, data)
}

var tmplFuncs = template.FuncMap{
	"humanize":   humanizeInt,
	"pathEscape": pathEscape,
	"htmlEscape": html.EscapeString,
}

var dirListTmpl = template.Must(
	template.New("dirList").
		Funcs(tmplFuncs).
		Parse(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">

<style>
table {
	border-collapse: collapse;
	text-align: left;
}

thead {
    background-color: #e2eef4;
    text-align: center;
}

th, td {
	padding-top: 0.3em;
	padding-bottom: 0.3em;
	padding-left: 0.3em;
	padding-right: 1em;
}

tbody tr:nth-child(odd) {
    background-color: #f8f8f8;
}

tbody tr:nth-child(even) {
    background-color: white;
}
</style>

</head>
<body>

<table>
<thead>
  <tr>
    <th>Name</th>
	<th>Size</th>
	<th>Last modified</th>
  </tr>
</thead>

<tbody>
{{range .Dirs -}}
  <tr>
    <td><code>
	  <a href="{{.Name | pathEscape}}{{if .IsDir}}/{{end}}">
	         {{- .Name | htmlEscape}}{{if .IsDir}}/{{end}}</a>
    </code></td>
	<td>{{if not .IsDir}}{{.Size | humanize}}{{end}}</td>
	<td>{{.ModTime.Format "2006-01-02 15:04:05"}}</td>
</tr>
{{- end}}
</tbody>
</table>

</body>
</html>
`))

func humanizeInt(i int64) string {
	if i > 1024*1024*1024 {
		return strconv.FormatInt(i/(1024*1024*1024), 10) + "G"
	}
	if i > 1024*1024 {
		return strconv.FormatInt(i/(1024*1024), 10) + "M"
	}
	if i > 1024 {
		return strconv.FormatInt(i/1024, 10) + "K"
	}
	return strconv.FormatInt(i, 10)
}

func pathEscape(path string) string {
	// This ensure that paths containing # and ? are escaped properly.
	u := url.URL{Path: path}
	return u.String()
}
