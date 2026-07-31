package internal

import (
	"errors"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name            string
		content         string
		wantFrontmatter string
		wantBody        string
		wantErr         error
	}{
		{
			name:            "frontmatter and body",
			content:         "---\nid: alpha\ntitle: Alpha\n---\n\n# Heading\n\nBody text.\n",
			wantFrontmatter: "id: alpha\ntitle: Alpha",
			wantBody:        "\n# Heading\n\nBody text.\n",
		},
		{
			name:            "no frontmatter",
			content:         "# Heading\n\nBody text.\n",
			wantFrontmatter: "",
			wantBody:        "",
			wantErr:         errNoFrontmatter,
		},
		{
			name:            "unterminated frontmatter",
			content:         "---\nid: alpha\ntitle: Alpha\n",
			wantFrontmatter: "",
			wantBody:        "",
			wantErr:         errUnclosedFrontmatter,
		},
		{
			name:            "empty body",
			content:         "---\nid: alpha\n---\n",
			wantFrontmatter: "id: alpha",
			wantBody:        "",
		},
		{
			name:            "no trailing newline after closing delimiter",
			content:         "---\nid: alpha\n---",
			wantFrontmatter: "id: alpha",
			wantBody:        "",
		},
		{
			name:            "empty frontmatter block",
			content:         "---\n\n---\nbody\n",
			wantFrontmatter: "",
			wantBody:        "body\n",
		},
		{
			name:            "only opening delimiter",
			content:         "---\n",
			wantFrontmatter: "",
			wantBody:        "",
			wantErr:         errUnclosedFrontmatter,
		},
		{
			name:            "body containing its own --- line",
			content:         "---\nid: alpha\n---\nbefore\n---\nafter\n",
			wantFrontmatter: "id: alpha",
			wantBody:        "before\n---\nafter\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := splitFrontmatter(tt.content)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if fm != tt.wantFrontmatter {
				t.Errorf("frontmatter = %q, want %q", fm, tt.wantFrontmatter)
			}
			if body != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}
