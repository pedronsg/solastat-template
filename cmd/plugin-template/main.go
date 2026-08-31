// plugin-template is the minimal starting point for a new solastat plugin:
// it registers with the core app's PluginHub (license handshake +
// solar-data subscription), serves a tiny status page, and registers that
// page with AppHub under its own route. Copy this directory, rename the
// module/package_id, and build your automation on top of OnSnapshot /
// ReadRegisters / WriteRegister.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/pedronsg/solastat-auth/pkg/auth"
	"github.com/pedronsg/solastat-template/pkg/pluginclient"
	orbitpluginpb "github.com/pedronsg/solastat-template/proto/gen"

	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/client"
	"github.com/OrbitOS-org/orbit-os-sdk-go/v26/logger"
)

const (
	logTag   = "plugin-template"
	pluginID = "plugin-template" // rename to your plugin's id, e.g. "relay"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	host := flag.String("host", "192.168.5.226", "OrbitOS device IP address")
	pluginHubSocket := flag.String("pluginhub-socket", pluginclient.DefaultSocketPath, "core PluginHub Unix socket path")
	httpPort := flag.Int("http-port", 9100, "HTTP status page port")
	route := flag.String("route", "/solastat-plugin-template", "AppHub route to register")
	flag.Parse()

	logger.Init(pluginID, "INFO", true)
	logger.Infof(logTag, "starting %s", pluginID)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Connect to the OrbitOS device daemon for whatever device services this
	// plugin needs (GPIO, sensors, ...). Not required if the plugin only
	// consumes solar data and Modbus registers via the core.
	deviceClient, err := client.NewClientAuto(*host)
	if err != nil {
		logger.Errorf(logTag, "connect to device: %v", err)
	} else {
		defer deviceClient.Close()
	}

	// Compute this device's hash independently — never trust the core to
	// tell us which device we're running on. Empty if the device connection
	// failed, in which case checkAuthorized below always reports false
	// (fail closed) rather than skipping verification.
	var deviceHash string
	if deviceClient != nil {
		serial, serr := deviceClient.SystemManager.GetSystemUuid()
		if serr != nil || serial == "" {
			serial, _ = deviceClient.SystemManager.GetBoardSerial()
		}
		if serial != "" {
			deviceHash = auth.DeviceHash(serial)
		}
	}
	if deviceHash == "" {
		logger.Errorf(logTag, "could not determine device identity — this plugin will never authorize")
	}

	// checkAuthorized is called by pluginclient with the raw key pool the
	// core is currently holding (opaque to it) — this plugin verifies each
	// key itself, against its own ID and independently-computed device
	// hash. The core's Register response is never trusted for this
	// decision, only for reporting the verdict back for Settings display.
	checkAuthorized := func(keys []string) bool {
		if deviceHash == "" {
			return false
		}
		for _, key := range keys {
			if auth.Authorizes(key, deviceHash, pluginID) {
				return true
			}
		}
		return false
	}

	// Connect to the core's PluginHub. Authorization can flip at runtime
	// (e.g. right after the user pastes a license key in Settings), so react
	// to it via OnAuthorization rather than checking it once at startup.
	hub := pluginclient.New(*pluginHubSocket, pluginID, "0.1.0", checkAuthorized)
	defer hub.Close()

	authorized := false
	hub.OnAuthorization(func(ok bool) {
		authorized = ok
		if ok {
			logger.Infof(logTag, "authorized")
		} else {
			logger.Warnf(logTag, "not authorized — activate this plugin in the core's Settings page")
		}
	})

	hub.OnSnapshot(func(snap *orbitpluginpb.SolarSnapshot) {
		if !authorized {
			return // fail closed: no automation while unlicensed
		}
		// TODO: react to snap.Readings here.
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"plugin_id":  pluginID,
			"authorized": authorized,
		})
	})

	srv := &http.Server{Addr: fmt.Sprintf(":%d", *httpPort), Handler: mux}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf(logTag, "http server: %v", err)
		}
	}()

	if deviceClient != nil {
		if err := deviceClient.AppHubManager.RegisterWebUI(fmt.Sprintf("127.0.0.1:%d", *httpPort), *route); err != nil {
			logger.Errorf(logTag, "apphub register: %v", err)
		} else {
			defer deviceClient.AppHubManager.UnregisterService()
		}
	}

	<-ctx.Done()
	logger.Infof(logTag, "shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutCtx)
}
