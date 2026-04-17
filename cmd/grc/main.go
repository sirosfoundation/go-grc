package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/cmd/grc/derive"
	"github.com/sirosfoundation/go-grc/cmd/grc/export"
	"github.com/sirosfoundation/go-grc/cmd/grc/initialize"
	"github.com/sirosfoundation/go-grc/cmd/grc/render"
	"github.com/sirosfoundation/go-grc/cmd/grc/status"
	"github.com/sirosfoundation/go-grc/cmd/grc/sync"
	"github.com/sirosfoundation/go-grc/cmd/grc/validate"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "grc",
		Short:   "Governance, Risk & Compliance toolchain",
		Version: fmt.Sprintf("%s (built %s)", Version, BuildTime),
	}

	rootCmd.PersistentFlags().StringP("root", "r", ".", "Path to compliance data root directory")

	rootCmd.AddCommand(initialize.NewCommand())
	rootCmd.AddCommand(sync.NewCommand())
	rootCmd.AddCommand(derive.NewCommand())
	rootCmd.AddCommand(render.NewCommand())
	rootCmd.AddCommand(export.NewCommand())
	rootCmd.AddCommand(status.NewCommand())
	rootCmd.AddCommand(validate.NewCommand())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
