// af-stack — the suite runtime entry point.
//
// Phase 0: placeholder. Real implementation lands in Phase 1 (issues #8-#15).
package main

import (
	"fmt"
	"os"
)

const version = "0.0.1"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("af-stack %s\n", version)
		return
	}
	fmt.Printf("af-stack %s — runtime not implemented yet (Phase 1)\n", version)
	fmt.Println("See https://github.com/Agent-Field/backai for the build roadmap.")
}
