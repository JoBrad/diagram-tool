package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSplitExportPath(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "existing.drawio")
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	noExt := filepath.Join(tmp, "plain")
	if err := os.WriteFile(noExt, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path, dir, stem string
	}{
		{tmp, tmp, ""},                                 // existing directory
		{existing, tmp, "existing"},                    // existing file
		{noExt, tmp, "plain"},                          // existing file without an extension
		{filepath.Join(tmp, "new.drawio"), tmp, "new"}, // new file
		{filepath.Join(tmp, "out", "my.diagram.json"), filepath.Join(tmp, "out"), "my.diagram"},
		{filepath.Join(tmp, "newdir"), filepath.Join(tmp, "newdir"), ""}, // new directory
		{"", "", ""},
	}
	for _, tt := range tests {
		dir, stem := SplitExportPath(tt.path)
		if dir != tt.dir || stem != tt.stem {
			t.Errorf("SplitExportPath(%q) = %q, %q; want %q, %q", tt.path, dir, stem, tt.dir, tt.stem)
		}
	}
}

func TestWriteFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "out")

	path, err := WriteFile(dir, "a.json", []byte("one"), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "a.json"); path != want {
		t.Errorf("path = %q, want %q", path, want)
	}

	if _, err := WriteFile(dir, "a.json", []byte("two"), false); err == nil {
		t.Error("WriteFile replaced an existing file without overwrite")
	}
	if got, _ := os.ReadFile(path); string(got) != "one" {
		t.Errorf("refused write changed the file to %q", got)
	}

	if _, err := WriteFile(dir, "a.json", []byte("two"), true); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "two" {
		t.Errorf("overwrite left %q, want %q", got, "two")
	}
}

func TestWriteFileErrors(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// A regular file where the directory should be fails MkdirAll.
	if _, err := WriteFile(filepath.Join(file, "sub"), "a.json", nil, false); err == nil {
		t.Error("WriteFile under a regular file: want an error")
	}
	// An existing directory at the target path passes Stat with overwrite set,
	// then fails the write itself.
	if err := os.Mkdir(filepath.Join(tmp, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFile(tmp, "taken", nil, true); err == nil {
		t.Error("WriteFile over a directory: want an error")
	}
}
