package main

import "testing"

func TestCheckGroupName(t *testing.T) {
	for _, ok := range []string{"contabo.acme.codejourney.io", "acme.example.com", "a-b.c1"} {
		if err := checkGroupName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "contabo", "Acme.Example.com", "a..b", "-a.b", "a_b.c", "acme.example.com."} {
		if err := checkGroupName(bad); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}
