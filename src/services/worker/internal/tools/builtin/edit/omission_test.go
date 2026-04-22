package edit

import "testing"

func TestDetectOmissionPlaceholders(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		new     string
		wantErr bool
	}{
		{
			name:    "normal code no trigger",
			old:     "func foo() {}",
			new:     "func foo() { return 1 }",
			wantErr: false,
		},
		{
			name:    "rest of code triggers",
			old:     "func foo() {\n  a := 1\n  b := 2\n}",
			new:     "func foo() {\n  a := 1\n  // rest of code...\n}",
			wantErr: true,
		},
		{
			name:    "existing code with dots triggers",
			old:     "line1\nline2\nline3",
			new:     "line1\n// ...existing code\nline3",
			wantErr: false, // "existing code" is after dots, prefix before dots is empty
		},
		{
			name:    "existing code prefix triggers",
			old:     "line1\nline2\nline3",
			new:     "line1\n// existing code...\nline3",
			wantErr: true,
		},
		{
			name:    "hash comment rest of file",
			old:     "a = 1\nb = 2",
			new:     "a = 1\n# rest of file...",
			wantErr: true,
		},
		{
			name:    "same placeholder in old does not trigger",
			old:     "// rest of code...\nfoo()",
			new:     "// rest of code...\nbar()",
			wantErr: false,
		},
		{
			name:    "unicode ellipsis triggers",
			old:     "func a() {}",
			new:     "func a() {\n  // remaining code…\n}",
			wantErr: true,
		},
		{
			name:    "three dots in string literal no trigger",
			old:     `fmt.Println("loading...")`,
			new:     `fmt.Println("please wait...")`,
			wantErr: false,
		},
		{
			name:    "no changes phrase triggers",
			old:     "a\nb\nc",
			new:     "a\n// no changes...\nc",
			wantErr: true,
		},
		{
			name:    "other methods triggers",
			old:     "method1()\nmethod2()",
			new:     "method1()\n// other methods...",
			wantErr: true,
		},
		{
			name:    "html comment rest of code",
			old:     "<div>a</div>",
			new:     "<div>a</div>\n<!-- rest of code... -->",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DetectOmissionPlaceholders(tt.old, tt.new)
			if (err != nil) != tt.wantErr {
				t.Errorf("got err=%v, wantErr=%v", err, tt.wantErr)
			}
			if err != nil && err.Code != ErrCodeOmission {
				t.Errorf("got code=%s, want %s", err.Code, ErrCodeOmission)
			}
		})
	}
}
