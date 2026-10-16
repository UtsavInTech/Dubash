package main

import "testing"

func TestCheckToken(t *testing.T) {
	strong := "Zq3v9Lx0pA7mN2wE5rT8yU1i"
	for _, c := range []struct {
		token, secret, publicURL, addr string
		ok                             bool
	}{
		{"", "", "", "127.0.0.1:8080", true}, // this computer only
		{"", "", "", "localhost:8080", true},
		{"change-me", "", "", "[::1]:8080", true},
		{"", "", "", ":8080", false}, // every interface
		{"change-me", "", "", "0.0.0.0:8080", false},
		{"short", "", "", ":8080", false},
		{strong, "", "", ":8080", true},
		{"", "", "https://subs.example.org", "127.0.0.1:8080", false}, // behind a public proxy
		{strong, "short", "", ":8080", false},
		{strong, strong + "x", "https://subs.example.org", ":8080", true},
	} {
		if err := checkToken(c.token, c.secret, c.publicURL, c.addr); (err == nil) != c.ok {
			t.Errorf("%+v: %v", c, err)
		}
	}
}
