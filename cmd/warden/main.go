package main

import (
	"os"

	"github.com/luizpedrini/forecast-warden/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
