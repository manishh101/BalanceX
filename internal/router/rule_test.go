package router

import (
	"net/http"
	"testing"
)

func TestParseRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    string
		req     *http.Request
		match   bool
		wantErr bool
	}{
		{
			name:  "Simple PathPrefix",
			rule:  `PathPrefix("/api")`,
			req:   makeReq("GET", "/api/users", nil),
			match: true,
		},
		{
			name:  "Simple PathPrefix double quotes",
			rule:  `PathPrefix("/api")`,
			req:   makeReq("GET", "/api/users", nil),
			match: true,
		},
		{
			name:  "Mismatch PathPrefix",
			rule:  `PathPrefix("/xyz")`,
			req:   makeReq("GET", "/api/users", nil),
			match: false,
		},
		{
			name:  "AND matching",
			rule:  `PathPrefix("/api") && Method("POST")`,
			req:   makeReq("POST", "/api/users", nil),
			match: true,
		},
		{
			name:  "AND not matching method",
			rule:  `PathPrefix("/api") && Method("POST")`,
			req:   makeReq("GET", "/api/users", nil),
			match: false,
		},
		{
			name:  "OR match first",
			rule:  `Path("/login") || Path("/signup")`,
			req:   makeReq("GET", "/login", nil),
			match: true,
		},
		{
			name:  "OR match second",
			rule:  `Path("/login") || Path("/signup")`,
			req:   makeReq("GET", "/signup", nil),
			match: true,
		},
		{
			name:  "Parentheses override precedence",
			rule:  `PathPrefix("/api") && (Method("POST") || Method("PUT"))`,
			req:   makeReq("PUT", "/api/update", nil),
			match: true,
		},
		{
			name:  "Header matcher",
			rule:  `Header("X-Internal", "true")`,
			req:   makeReq("GET", "/test", map[string]string{"X-Internal": "true"}),
			match: true,
		},
		{
			name:    "Invalid syntax - missing paren",
			rule:    `PathPrefix("/api"`,
			wantErr: true,
		},
		{
			name:    "Invalid matcher name",
			rule:    `Unknown("/api")`,
			wantErr: true,
		},
		{
			name:    "Unterminated quotes",
			rule:    `PathPrefix("/api)`,
			wantErr: true,
		},
		{
			name:    "Missing arguments",
			rule:    `Header("X-Internal")`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := ParseRule(tt.rule)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRule() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				if got := matcher(tt.req); got != tt.match {
					t.Errorf("Matcher() = %v, want %v", got, tt.match)
				}
			}
		})
	}
}

func makeReq(method, path string, headers map[string]string) *http.Request {
	req, _ := http.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}
