package cli

import (
	"fmt"
	"io"
	"os"
)

// stdin returns the App's stdin reader, falling back to os.Stdin.
func stdin(app *App) io.Reader {
	if app.Stdin != nil {
		return app.Stdin
	}
	return os.Stdin
}

// interactive returns whether the CLI is attached to an interactive terminal.
// Uses app.IsTerminal when set; otherwise checks whether os.Stdin is a character device.
func interactive(app *App) bool {
	if app.IsTerminal != nil {
		return app.IsTerminal()
	}
	return isTerminal()
}

// readAnswer reads a single whitespace-delimited token from the App's stdin.
func readAnswer(app *App) string {
	var s string
	fmt.Fscanln(stdin(app), &s)
	return s
}
