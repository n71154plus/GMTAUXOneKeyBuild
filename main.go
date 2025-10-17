package main

import (
	"log"

	"GMTAUXOneKeyBuild/ui"
)

func main() {
	if err := ui.NewApp().Run(); err != nil {
		log.Fatalf("failed to run application: %v", err)
	}
}
