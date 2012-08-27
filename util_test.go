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
