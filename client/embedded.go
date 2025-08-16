package main

import _ "embed"

//go:embed ship.jpg
var embeddedSplashImage []byte

// HasEmbeddedSplash returns true if we have an embedded splash image
func HasEmbeddedSplash() bool {
	return len(embeddedSplashImage) > 0
}