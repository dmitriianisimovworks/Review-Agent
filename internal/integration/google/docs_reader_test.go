package google

import "testing"

func TestExtractDocumentID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "google docs url",
			input: "https://docs.google.com/document/d/abc123DEF456/edit",
			want:  "abc123DEF456",
		},
		{
			name:  "bare document id",
			input: "abc123DEF456",
			want:  "abc123DEF456",
		},
		{
			name:    "invalid url",
			input:   "https://docs.google.com/document/u/0/",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := ExtractDocumentID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}
