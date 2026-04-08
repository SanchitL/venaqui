package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseBatchFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
		wantErr string
	}{
		{
			name:    "single link",
			content: "https://example.com/file.zip\n",
			want:    []string{"https://example.com/file.zip"},
		},
		{
			name:    "multiple links",
			content: "https://example.com/a.zip\nhttps://example.com/b.zip\nhttps://example.com/c.zip\n",
			want:    []string{"https://example.com/a.zip", "https://example.com/b.zip", "https://example.com/c.zip"},
		},
		{
			name:    "skips empty lines",
			content: "https://example.com/a.zip\n\n\nhttps://example.com/b.zip\n",
			want:    []string{"https://example.com/a.zip", "https://example.com/b.zip"},
		},
		{
			name:    "skips comment lines",
			content: "# this is a comment\nhttps://example.com/a.zip\n# another comment\nhttps://example.com/b.zip\n",
			want:    []string{"https://example.com/a.zip", "https://example.com/b.zip"},
		},
		{
			name:    "trims whitespace",
			content: "  https://example.com/a.zip  \n\thttps://example.com/b.zip\t\n",
			want:    []string{"https://example.com/a.zip", "https://example.com/b.zip"},
		},
		{
			name:    "empty file",
			content: "",
			wantErr: "batch file contains no links",
		},
		{
			name:    "only comments and blanks",
			content: "# comment\n\n# another\n\n",
			wantErr: "batch file contains no links",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "links.txt")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatal(err)
			}

			got, err := parseBatchFile(path)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d links, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("link[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseBatchFile_FileNotFound(t *testing.T) {
	_, err := parseBatchFile("/nonexistent/path/links.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
	if !contains(err.Error(), "failed to open batch file") {
		t.Fatalf("expected 'failed to open batch file' error, got %q", err.Error())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
