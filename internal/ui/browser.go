package ui

import (
	"errors"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser hands a web URL to the desktop's default handler. Only http and
// https reach the handler, whatever the caller resolved.
func OpenBrowser(target string) error {
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("only http and https URLs can be opened")
	}
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", target)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return err
	}
	// The handler outlives this process; do not wait for it.
	go command.Wait()
	return nil
}
