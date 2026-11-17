package clitest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

// Route is one CLI invocation and the request it must send.
type Route struct {
	Name   string
	Args   []string
	Stdin  string
	Status int
	// Answer is the response body; list routes need an array because the
	// generated client decodes it.
	Answer string
	Want   Request
}

// CheckRoutes runs every route against a fresh server and compares the one
// request it sent, with the session token, and the printed answer.
func CheckRoutes(t *testing.T, routes []Route) {
	t.Helper()
	for _, r := range routes {
		t.Run(r.Name, func(t *testing.T) {
			status := r.Status
			if status == 0 {
				status = http.StatusOK
			}
			answer := r.Answer
			if answer == "" && status != http.StatusNoContent {
				answer = `{"name":"x"}`
			}
			server := NewServer(t, status, answer)
			var stdin io.Reader
			if r.Stdin != "" {
				stdin = strings.NewReader(r.Stdin)
			}
			out, err := RunAuthenticated(t, server, stdin, r.Args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", r.Args, err, out)
			}
			got := server.Requests()
			if len(got) != 1 {
				t.Fatalf("%v sent %d requests, want 1: %+v", r.Args, len(got), got)
			}
			want := r.Want
			want.Authorization = "Bearer session-token"
			if want.RawPath == "" {
				want.RawPath = want.Path
			}
			got[0].Query = normalQuery(t, got[0].Query)
			if !reflect.DeepEqual(got[0], want) {
				t.Fatalf("%v sent\n %+v\nwant\n %+v", r.Args, got[0], want)
			}
			if answer == "" {
				if out != "204 No Content\n" {
					t.Fatalf("printed %q for an empty answer", out)
				}
			} else if !SameJSON(out, answer) {
				t.Fatalf("printed %q, want the answer %q", out, answer)
			}
		})
	}
}

// normalQuery drops the empty parameters the generated client sends for
// unset optional flags and sorts the rest.
func normalQuery(t *testing.T, raw string) string {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("query %q: %v", raw, err)
	}
	for key, value := range values {
		if len(value) == 1 && value[0] == "" {
			values.Del(key)
		}
	}
	return values.Encode()
}

// SameJSON reports whether printed is the JSON value want and nothing else;
// formatting whitespace does not count.
func SameJSON(printed, want string) bool {
	var got, expected any
	if json.Unmarshal([]byte(printed), &got) != nil || json.Unmarshal([]byte(want), &expected) != nil {
		return false
	}
	return reflect.DeepEqual(got, expected)
}
