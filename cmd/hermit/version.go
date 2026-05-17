package main

import "fmt"

// Version is set at build time via ldflags: -X main.Version=x.y.z
var Version = "dev"

func cmdVersion() {
	fmt.Println("hermit", Version)
}
