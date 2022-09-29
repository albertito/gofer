package util

import (
	"os"
	"strings"
	"testing"

	"blitiri.com.ar/go/gofer/config"
)

func TestLoadCertsFromDir(t *testing.T) {
	// The data in testdata/ is crafted to test some of the corner cases of
	// LoadCertsFromDir.

	// Incorrect/missing some of the files.
	c, err := LoadCertsFromDir("testdata/certs/")
	if c != nil {
		t.Errorf("expected nil config, got %v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "no certificates found") {
		t.Errorf("expected 'no certificates found' error, got: %v", err)
	}

	// Invalid PEM certificates.
	c, err = LoadCertsFromDir("testdata/badcerts/")
	if c != nil {
		t.Errorf("expected nil config, got %v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "error loading pair") {
		t.Errorf("expected 'error loading pair' error, got: %v", err)
	}

	// Empty directory.
	c, err = LoadCertsFromDir("testdata/empty/")
	if c != nil {
		t.Errorf("expected nil config, got %v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "no certificates found") {
		t.Errorf("expected 'no certificates found' error, got: %v", err)
	}

	// Non-existent directory.
	c, err = LoadCertsFromDir("testdata/doesnotexist/")
	if c != nil {
		t.Errorf("expected nil config, got %v", c)
	}
	if err == nil || !strings.Contains(err.Error(), "ReadDir") {
		t.Errorf("expected ReadDir error, got: %v", err)
	}
}

func TestCacheIsWriteableCheck(t *testing.T) {
	conf := config.HTTPS{
		AutoCerts: config.AutoCerts{
			CacheDir: "/proc/should/not/be/allowed",
		},
	}
	c, err := LoadCertsForHTTPS(conf)
	if err == nil || !strings.Contains(err.Error(), "error writing") {
		t.Errorf("expected 'error writing to the autocert cache', got: %v / %v",
			c, err)
	}

	conf.AutoCerts.CacheDir = "testdata/.TestCacheIsWriteableCheck_dir"
	_, err = LoadCertsForHTTPS(conf)
	if err != nil {
		t.Errorf("failed to write on test directory: %v", err)
	}
}

func TestCachePath(t *testing.T) {
	checkEq := func(desc, cd string, expected string) {
		if c := cachePath(cd); c != expected {
			t.Errorf("%s: expected %q, got %q", desc, expected, c)
		}
	}

	checkEq("config dir is set", "/some/path/", "/some/path/")

	{
		orig := os.Getenv("CACHE_DIRECTORY")
		os.Setenv("CACHE_DIRECTORY", "/my/cache")
		checkEq("using $CACHE_DIRECTORY", "", "/my/cache/gofer-autocert-cache")
		os.Setenv("CACHE_DIRECTORY", orig)
	}

	{
		orig := os.Getenv("XDG_CACHE_HOME")
		os.Setenv("XDG_CACHE_HOME", "/xdg/cache")
		checkEq("using os.UserCacheDir", "", "/xdg/cache/gofer-autocert-cache")
		os.Setenv("XDG_CACHE_HOME", orig)
	}

	{
		origxdg := os.Getenv("XDG_CACHE_HOME")
		os.Unsetenv("XDG_CACHE_HOME")
		orighome := os.Getenv("HOME")
		os.Unsetenv("HOME")
		checkEq("last resort", "", "gofer-autocert-cache")
		os.Setenv("HOME", orighome)
		os.Setenv("XDG_CACHE_HOME", origxdg)
	}
}
