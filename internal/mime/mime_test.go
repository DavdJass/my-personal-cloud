package mime

import (
	"strings"
	"testing"
)

func TestDetectFromBytes(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		wantType string
	}{
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"PNG", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"GIF", []byte("GIF89a"), "image/gif"},
		{"plain text", []byte("hello world"), "text/plain"},
		{"HTML", []byte("<!DOCTYPE html>\n<html>"), "text/html"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFromBytes(tt.data)
			if !strings.HasPrefix(got, strings.Split(tt.wantType, "/")[0]) {
				t.Errorf("DetectFromBytes(%q) = %q, want %q prefix", tt.name, got, tt.wantType)
			}
		})
	}
}

func TestIsImage(t *testing.T) {
	if !IsImage("image/jpeg") {
		t.Error("expected image/jpeg to be image")
	}
	if !IsImage("image/png") {
		t.Error("expected image/png to be image")
	}
	if !IsImage("image/gif") {
		t.Error("expected image/gif to be image")
	}
	if IsImage("text/plain") {
		t.Error("expected text/plain to NOT be an image")
	}
	if IsImage("application/pdf") {
		t.Error("expected application/pdf to NOT be an image")
	}
}

func TestExtensionForMIME(t *testing.T) {
	ext := ExtensionForMIME("image/jpeg")
	if ext == "" {
		t.Error("expected non-empty extension for image/jpeg")
	}
	ext = ExtensionForMIME("application/pdf")
	if ext != ".pdf" {
		t.Errorf("expected .pdf, got %q", ext)
	}
}

func TestSniffSize(t *testing.T) {
	if SniffSize != 512 {
		t.Errorf("expected SniffSize=512, got %d", SniffSize)
	}
}
