package files

import (
	"testing"
)

func TestNormalizeParent(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"/foo", "/foo"},
		{"foo", "/foo"},
		{"/foo/bar", "/foo/bar"},
		{"/foo/../bar", "/bar"},
		{"/foo/./bar", "/foo/bar"},
	}
	for _, tt := range tests {
		got := normalizeParent(tt.input)
		if got != tt.want {
			t.Errorf("normalizeParent(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"normal.txt", "normal.txt"},
		{"../etc/passwd", "passwd"},
		{"foo/../../../bar.txt", "bar.txt"},
		{"   spaced   ", "spaced"},
		{".", ""},
		{"..", ""},
		{"/", ""},
		{"a/b/c.txt", "c.txt"},
		{"C:\\Windows\\file.exe", "file.exe"},
	}
	for _, tt := range tests {
		got := sanitizeFilename(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFolderFullPath(t *testing.T) {
	tests := []struct {
		parent string
		name   string
		want   string
	}{
		{"/", "docs", "/docs"},
		{"/docs", "photos", "/docs/photos"},
		{"/a/b", "c", "/a/b/c"},
	}
	for _, tt := range tests {
		got := folderFullPath(tt.parent, tt.name)
		if got != tt.want {
			t.Errorf("folderFullPath(%q, %q) = %q, want %q", tt.parent, tt.name, got, tt.want)
		}
	}
}

func TestBoolInt(t *testing.T) {
	if boolInt(true) != 1 {
		t.Error("boolInt(true) should be 1")
	}
	if boolInt(false) != 0 {
		t.Error("boolInt(false) should be 0")
	}
}
