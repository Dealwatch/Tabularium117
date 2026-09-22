package main

import (
	"log/slog"
	"os/exec"
	"runtime"
)

// openBrowser asks the desktop to open url in the user's default browser.
//
// Every failure is logged and swallowed. The URL is on the console as well,
// and a machine that cannot be asked - a server, a remote session, a Windows
// install without the shell - is no reason for the program to stop: the whole
// point of the tool is the page it is already serving.
func openBrowser(url string, log *slog.Logger) {
	name, args := browserCommand(url)
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		log.Warn("cannot open a browser; open the URL yourself", "err", err, "url", url)
		return
	}
	// The helper hands the URL to the desktop and exits; waiting for it in
	// the background keeps no zombie process behind.
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Debug("the browser helper reported a failure", "err", err, "url", url)
		}
	}()
}

// browserCommand is the per-platform way to open a URL. Only Windows is a
// release target, but development happens on Linux and macOS.
func browserCommand(url string) (string, []string) {
	switch runtime.GOOS {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}
