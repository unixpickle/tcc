package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/unixpickle/tcc/resideo"
	"github.com/unixpickle/tcc/tcc"
	"github.com/unixpickle/tcc/thermostat"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	rootFlag := flag.String("root", "", "single path segment under which to serve the UI and API")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() != 0 {
		usage()
		os.Exit(2)
	}
	root, err := cleanRoot(*rootFlag)
	if err != nil {
		log.Fatal(err)
	}
	backends, err := configuredBackends(os.Getenv, map[string]backendFactory{
		"TCC":     func(u, p string) (thermostat.Backend, error) { return tcc.NewBackend(u, p) },
		"RESIDEO": func(u, p string) (thermostat.Backend, error) { return resideo.NewBackend(u, p) },
	})
	if err != nil {
		log.Fatal(err)
	}
	appHandler := newHandler(backends)
	handler := mountRoot(appHandler, root)
	if root == "" {
		log.Printf("serving thermostat web UI at http://localhost%s/", displayAddr(*addr))
	} else {
		log.Printf("serving thermostat web UI at http://localhost%s/%s/", displayAddr(*addr), root)
	}
	server := &http.Server{
		Addr:    *addr,
		Handler: handler,
	}
	server.SetKeepAlivesEnabled(false)
	log.Fatal(server.ListenAndServe())
}

func cleanRoot(value string) (string, error) {
	root := strings.Trim(value, "/")
	if root == "" {
		return "", nil
	}
	if strings.Contains(root, "/") {
		return "", fmt.Errorf("root must be a single directory name, got %q", value)
	}
	if strings.Contains(root, "..") {
		return "", fmt.Errorf("root cannot contain '..', got %q", value)
	}
	return root, nil
}

func mountRoot(handler http.Handler, root string) http.Handler {
	if root == "" {
		return handler
	}
	mount := "/" + root
	mux := http.NewServeMux()
	mux.Handle(mount+"/", http.StripPrefix(mount, handler))
	mux.HandleFunc(mount, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, mount+"/", http.StatusMovedPermanently)
	})
	return mux
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return addr
	}
	return " " + addr
}

func usage() {
	output := flag.CommandLine.Output()
	fmt.Fprintf(output, "usage: %s [-addr address] [-root name]\n", os.Args[0])
	fmt.Fprintln(output)
	fmt.Fprintln(output, "environment: TCC_USERNAME + TCC_PASSWORD and/or RESIDEO_USERNAME + RESIDEO_PASSWORD")
	fmt.Fprintln(output)
	fmt.Fprintln(output, "flags:")
	flag.PrintDefaults()
}

type backendFactory func(username, password string) (thermostat.Backend, error)

func configuredBackends(getenv func(string) string, factories map[string]backendFactory) (thermostat.Collection, error) {
	type credentials struct{ name, username, password string }
	var configs []credentials
	for _, name := range []string{"TCC", "RESIDEO"} {
		u, p := getenv(name+"_USERNAME"), getenv(name+"_PASSWORD")
		if (u == "") != (p == "") {
			return nil, fmt.Errorf("%s_USERNAME and %s_PASSWORD must be set together", name, name)
		}
		if u != "" {
			configs = append(configs, credentials{name, u, p})
		}
	}
	if len(configs) == 0 {
		return nil, fmt.Errorf("set TCC_USERNAME and TCC_PASSWORD and/or RESIDEO_USERNAME and RESIDEO_PASSWORD")
	}
	result := thermostat.Collection{}
	for _, c := range configs {
		b, err := factories[c.name](c.username, c.password)
		if err != nil {
			return nil, fmt.Errorf("%s login: %w", c.name, err)
		}
		result[strings.ToLower(c.name)] = b
	}
	return result, nil
}
