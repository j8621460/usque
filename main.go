package main

import (
	"fmt"
	"os"

	"github.com/Diniboy1123/usque/cmd"
	"github.com/Diniboy1123/usque/internal"
)

func main() {
	if err := internal.EnableSpeculationStoreBypassMitigation(); err != nil {
		fmt.Println("Warning: failed to enable speculation store bypass mitigation:", err)
	}

	if err := cmd.Execute(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
