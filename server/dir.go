package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"blitiri.com.ar/go/gofer/config"
)

type FileSystem struct {
	fs http.FileSystem

	opts config.DirOpts
}

func NewFS(fs http.FileSystem, opts config.DirOpts) *FileSystem {
	return &FileSystem{
		fs:   fs,
		opts: opts,
	}
}

func ListingEnabled(opts *config.DirOpts, name string) bool {
	if name == "" {
		name = "/"
	}
	name = filepath.Clean(name)

	longestP := ""
	value := false
	for p, val := range opts.Listing {
		p = filepath.Clean(p)
		if strings.HasPrefix(name, p) && len(p) > len(longestP) {
			longestP = p
			value = val
		}
	}

	return value
}

func (fs *FileSystem) Open(name string) (http.File, error) {
	for _, re := range fs.opts.Exclude {
		if re.MatchString(name) {
			return nil, os.ErrNotExist
		}
	}

	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	f = wrappedFile{File: f, name: name, opts: &fs.opts}

	if ListingEnabled(&fs.opts, name) {
		return f, nil
	}

	// If it's not a directory, let it be.
	if s, _ := f.Stat(); s == nil || !s.IsDir() {
		return f, nil
	}

	// It's a directory, and listing not allowed.
	// However, if there is an index.html, we let it be served.
	index := filepath.Join(name, "index.html")
	if idxf, err := fs.fs.Open(index); err == nil {
		idxf.Close()
		return f, err
	}

	f.Close()
	return nil, os.ErrNotExist
}

type wrappedFile struct {
	http.File
	name string
	opts *config.DirOpts
}

func (f wrappedFile) Readdir(count int) ([]os.FileInfo, error) {
	if !ListingEnabled(f.opts, f.name) {
		return nil, os.ErrNotExist
	}

	// Exclude files from listings too.
	all, err := f.File.Readdir(count)
	var fis []os.FileInfo
outer:
	for _, fi := range all {
		for _, re := range f.opts.Exclude {
			name := filepath.Join(f.name, fi.Name())
			if re.MatchString(name) {
				continue outer
			}
		}
		fis = append(fis, fi)
	}
	return fis, err
}
