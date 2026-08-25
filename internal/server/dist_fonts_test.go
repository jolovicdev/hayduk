package server

import (
	"io/fs"
	"path"
	"testing"
)

// The embedded UI ships subset fonts: full families and svg fallbacks push
// the binary past 8 MiB for glyphs the console never draws.
func TestEmbeddedFontsAreSubset(t *testing.T) {
	const maxFont = 100 << 10
	err := fs.WalkDir(distFileSystem(), "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		switch path.Ext(p) {
		case ".svg":
			t.Errorf("%s: svg font fallbacks do not belong in the dist", p)
		case ".woff", ".woff2", ".ttf":
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.Size() > maxFont {
				t.Errorf("%s: %d bytes, want <= %d (subset fonts only)", p, info.Size(), maxFont)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
