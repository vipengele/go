package main

import "os"

func main() {
	os.Exit(newApp(prodDeps()).Execute())
}
