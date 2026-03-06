package main

//go:generate bash gen-imports.sh

import (
	"microcoreos-go/core"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env if present. In production env vars are set by the OS/container,
	// so this is a no-op (godotenv never overwrites existing vars).
	godotenv.Load() //nolint:errcheck

	k := core.NewKernel()
	if err := k.Boot(); err != nil {
		panic(err)
	}
	k.WaitForShutdown()
}
