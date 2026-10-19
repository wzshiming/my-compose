package runner

import "testing"

func TestQuote(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{"nil", nil, ""},
		{"empty", []string{}, ""},
		{"safe tokens", []string{"fake", "run", "--label=com.x/y:z,v", "a@b%c+d=e", "A-Z_0.9"}, "fake run --label=com.x/y:z,v a@b%c+d=e A-Z_0.9"},
		{"space", []string{"a b"}, "'a b'"},
		{"single quote", []string{"it's"}, `'it'\''s'`},
		{"empty token", []string{""}, "''"},
		{"dollar", []string{"$HOME"}, "'$HOME'"},
		{"glob", []string{"*"}, "'*'"},
		{"non-ascii", []string{"你好"}, "'你好'"},
		{"double quote and backslash", []string{`a"b\c`}, `'a"b\c'`},
		{"tab", []string{"a\tb"}, "'a\tb'"},
		{"mixed", []string{"a", "b c"}, "a 'b c'"},
		{"empty among tokens", []string{"a", "", "b"}, "a '' b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Quote(tt.argv); got != tt.want {
				t.Errorf("Quote(%q) = %s, want %s", tt.argv, got, tt.want)
			}
		})
	}
}
