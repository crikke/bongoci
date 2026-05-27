// This file ensures cobra and its dependencies are included in go.mod and vendor.
// The blank import can be removed once cobra is imported in actual code.
package main

import (
	_ "github.com/spf13/cobra"
)
