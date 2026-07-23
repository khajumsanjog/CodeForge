package cmd

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	// Version is injected at build time using ldflags
	Version = "1.2.0"
)

var rootCmd = &cobra.Command{
	Use:     "codeforge",
	Short:   "CodeForge CI/CD Daemon and Dashboard Client",
	Version: Version,
	Long: `CodeForge is a complete, production-ready CI/CD daemon with a custom DSL (.kzm files), 
a secure encrypted credential vault, and a beautiful Fyne desktop GUI dashboard.

If run without arguments, it will launch the GUI automatically.`,
	Run: func(cmd *cobra.Command, args []string) {
		PrintBanner()
		runGUI()
	},
}

// PrintBanner prints a vibrant ASCII banner for CodeForge in terminal.
func PrintBanner() {
	cyan := color.New(color.FgCyan, color.Bold).SprintFunc()
	purple := color.New(color.FgMagenta, color.Bold).SprintFunc()
	white := color.New(color.FgWhite).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Println()
	fmt.Println(purple("  _____            _       ______                            "))
	fmt.Println(purple(" / ____|          | |     |  ____|                           "))
	fmt.Println(cyan("| |     ___   __| | ___  | |__  ___  _ __ __ _  ___          "))
	fmt.Println(cyan("| |    / _ \\ / _` |/ _ \\ |  __|/ _ \\| '__/ _` |/ _ \\         "))
	fmt.Println(cyan("| |___| (_) | (_| |  __/ | |  | (_) | | | (_| |  __/         "))
	fmt.Println(purple(" \\_____\\___/ \\__,_|\\___| |_|   \\___/|_|  \\__, |\\___|         "))
	fmt.Println(purple("                                          __/ |              "))
	fmt.Println(purple("                                         |___/               "))
	fmt.Printf("  %s %s | %s\n", white("CodeForge CI/CD Engine"), yellow("v"+Version), cyan("KhajumSanjog"))
	fmt.Println(color.HiBlackString("  -------------------------------------------------------------"))
	fmt.Println()
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	defer func() {
		if r := recover(); r != nil {
			stack := string(debug.Stack())
			color.Red("\n================================================================================")
			color.Red("  🚨 CODEFORGE UNHANDLED TERMINAL EXCEPTION")
			color.Red("  Error: %v", r)
			color.Red("--------------------------------------------------------------------------------")
			color.Yellow("%s", stack)
			color.Red("================================================================================\n")
			os.Exit(1)
		}
	}()

	if err := rootCmd.Execute(); err != nil {
		color.Red("CodeForge CLI Exception: %v", err)
		os.Exit(1)
	}
}
