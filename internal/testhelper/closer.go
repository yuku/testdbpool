package testhelper

import "io"

// MustClose closes the given closer and panics if there's an error.
// This is useful in tests where we don't want to handle close errors.
func MustClose(c io.Closer) {
	if err := c.Close(); err != nil {
		panic(err)
	}
}
