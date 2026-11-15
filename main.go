// boox-debloat: a small local app for safely debloating the Onyx Boox
// C69 M Star while keeping the sleep screen working.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"time"
)

// Set by the release build.
var version = "dev"

// The app stops this long after the page was last open.
const idleTimeout = 5 * time.Minute

func main() {
	demo := flag.Bool("demo", false, "try the app with a pretend tablet; nothing is sent to a real device")
	noBrowser := flag.Bool("no-browser", false, "don't open the browser automatically")
	port := flag.Int("port", 0, "port to listen on (default: any free port)")
	serial := flag.String("serial", "", "use this device when several are connected")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("boox-debloat", version)
		return
	}

	cat, err := loadCatalog()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	var be Backend
	if *demo {
		be = newDemoBackend(cat)
	} else {
		be = &adbBackend{path: findAdb()}
		if !be.Available() {
			fmt.Println("adb wasn't found. The page will show how to install it.")
		}
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, "Couldn't start:", err)
		os.Exit(1)
	}

	s := &Server{
		cat:     cat,
		be:      be,
		token:   newToken(),
		host:    ln.Addr().String(),
		demo:    *demo,
		serial:  *serial,
		version: version,
	}
	srv := &http.Server{Handler: s.routes(), ReadHeaderTimeout: 10 * time.Second}

	url := fmt.Sprintf("http://%s/#%s", s.host, s.token)
	fmt.Println("boox-debloat", version)
	if *demo {
		fmt.Println("Demo mode: nothing is sent to a real tablet.")
	}
	fmt.Println("Open this address if your browser doesn't open by itself:")
	fmt.Println("  " + url)
	fmt.Println("Press Ctrl+C to stop.")

	go srv.Serve(ln)
	if !*noBrowser {
		openBrowser(url)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	start := time.Now()
	for {
		select {
		case <-stop:
			shutdown(srv)
			return
		case <-tick.C:
			last := s.lastPing.Load()
			idleSince := start
			if last > 0 {
				idleSince = time.Unix(last, 0)
			}
			if time.Since(idleSince) > idleTimeout {
				fmt.Println("The page was closed, so boox-debloat stopped.")
				shutdown(srv)
				return
			}
		}
	}
}

func shutdown(srv *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}
