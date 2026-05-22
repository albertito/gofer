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

	// On-disk root of the served directory. Used by the PUT/DELETE handlers
	// to resolve request paths to filesystem paths. Empty if the underlying
	// http.FileSystem is not backed by a local directory.
	root string

	opts config.DirOpts
}

func NewFS(root string, fs http.FileSystem, opts config.DirOpts) *FileSystem {
	return &FileSystem{
		fs:   fs,
		root: root,
		opts: opts,
	}
}

// prefixMatch returns the value associated with the longest prefix of name in
// m, or false if no prefix matches.
func prefixMatch(m map[string]bool, name string) bool {
	if name == "" {
		name = "/"
	}
	name = filepath.Clean(name)

	longestP := ""
	value := false
	for p, val := range m {
		p = filepath.Clean(p)
		if strings.HasPrefix(name, p) && len(p) > len(longestP) {
			longestP = p
			value = val
		}
	}

	return value
}

func ListingEnabled(opts *config.DirOpts, name string) bool {
	return prefixMatch(opts.Listing, name)
}

func PutEnabled(opts *config.DirOpts, name string) bool {
	return prefixMatch(opts.Put, name)
}

func DeleteEnabled(opts *config.DirOpts, name string) bool {
	return prefixMatch(opts.Delete, name)
}

// localPath resolves a URL path to an absolute on-disk path under fs.root.
// It returns an error if writes are not supported (fs.root is unset) or if
// the resulting path would escape the root.
func (fs *FileSystem) localPath(name string) (string, error) {
	if fs.root == "" {
		return "", os.ErrPermission
	}
	absRoot, err := filepath.Abs(fs.root)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(absRoot, filepath.FromSlash(name))
	rel, err := filepath.Rel(absRoot, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", os.ErrPermission
	}
	return abs, nil
}

func (fs *FileSystem) excluded(name string) bool {
	for _, re := range fs.opts.Exclude {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

func (fs *FileSystem) Open(name string) (http.File, error) {
	if fs.excluded(name) {
		return nil, os.ErrNotExist
	}

	f, err := fs.fs.Open(name)
	if err != nil {
		return nil, err
	}

	// If it's not a directory, let it be.
	if s, _ := f.Stat(); s == nil || !s.IsDir() {
		return f, nil
	}

	f = wrappedFile{File: f, name: name, opts: &fs.opts}

	if ListingEnabled(&fs.opts, name) {
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
