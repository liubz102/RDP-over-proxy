// RDP over Proxy runs Windows Remote Desktop (mstsc.exe) through a proxy.
// mstsc cannot use proxies itself, so each connection gets a local loopback
// tunnel that mstsc connects to, and the tunnel carries the traffic through
// SOCKS5, HTTP or a V2Ray-family proxy.
package main

import (
	"embed"
	"log"

	"github.com/liubz102/RDP-over-proxy/internal/app"
)

// version is the application version. Release builds set it with
// -ldflags "-X main.version=<tag>"; the default is the version the project
// owner chose. Do not change it without the owner's say-so.
var version = "0.1.0"

// assets is the built frontend. Wails serves it to the window.
//
//go:embed all:frontend/dist
var assets embed.FS

// appIcon is the same icon the build puts into the exe. The tray takes the
// image in it that matches the small icon size of the display.
//
//go:embed build/windows/icon.ico
var appIcon []byte

func main() {
	err := app.Run(app.Options{
		Version: version,
		Assets:  assets,
		Icon:    appIcon,
	})
	if err != nil {
		log.Fatal(err)
	}
}
