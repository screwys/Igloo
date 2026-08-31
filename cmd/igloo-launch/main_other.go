//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("igloo-launch is only available on Windows")
}
