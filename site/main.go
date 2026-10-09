// Command site builds the PEGO web site: a landing page, the documentation rendered from the
// repository's Markdown, an API reference generated from the Go source, and the playground.
//
// Usage (from the site directory):
//
//	go run . [-repo ..] [-out dist] [-wasm=true] [-check=true] [-serve localhost:8080]
//
// Everything is generated from the repository at build time; nothing in the output is meant to be
// edited or committed. See docs/guide/playground.md and docs/design/016-web-site-and-playground.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func main() {
	var cfg Config
	previous, deploy := deploymentURLs(os.Getenv)
	flag.StringVar(&cfg.Repo, "repo", "..", "root of the PEGO repository")
	flag.StringVar(&cfg.Out, "out", "dist", "output directory (replaced by each build)")
	flag.BoolVar(&cfg.Wasm, "wasm", true, "build the playground's WebAssembly binary (needs the go command)")
	flag.BoolVar(&cfg.Check, "check", true, "fail if a generated page links to a missing page or anchor")
	flag.StringVar(&cfg.GitHub, "github", "https://github.com/ornew/pego", "repository URL for links to source files")
	flag.StringVar(&cfg.Ref, "ref", "main", "branch or tag for links to source files")
	flag.StringVar(&cfg.PreviousSite, "previous-site", previous, "retain the current WASM/runtime pair from this published site URL (automatic for Netlify production)")
	flag.StringVar(&cfg.DeployURL, "deploy-url", deploy, "immutable URL of this deploy, for fetching its assets on the next production build")
	serve := flag.String("serve", "", "after building, serve the site at this address (for example localhost:8080)")
	flag.Parse()

	st, err := Build(cfg)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "site: %d pages, %d files, %.1f MB in %s\n", st.Pages, st.Files, float64(st.Bytes)/1e6, cfg.Out)
	if *serve != "" {
		fmt.Fprintf(os.Stderr, "site: serving %s at http://%s/\n", cfg.Out, *serve)
		log.Fatal(http.ListenAndServe(*serve, http.FileServer(http.Dir(cfg.Out))))
	}
}
