package main

import (
	"testing"
)

func TestIsDigit(t *testing.T) {
	for i := 0; i < 10; i++ {
		digit := '0' + byte(i)
		letter := 'a' + byte(i)

		if IsDigit(digit) != true {
			t.Errorf("%c should return true", digit)
		}

		if IsDigit(letter) == true {
			t.Errorf("%c should return false", letter)
		}
	}
}

func TestHost2Domain(t *testing.T) {
	var testData = []struct {
		host   string
		domain string
	}{
		{"google.com", "google.com"},
		{"asdf.www.google.com", "google.com"},
		{"asdf.www.google.com", "google.com"},
		{"google.com:80", "google.com"},
		{"account.google.com:443", "google.com"},
	}

	for _, td := range testData {
		if host2Domain(td.host) != td.domain {
			t.Errorf("%s should return %v", td.host, td.domain)
		}
	}
}
