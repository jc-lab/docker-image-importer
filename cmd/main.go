package main

import (
	"context"
	"flag"
	"github.com/jc-lab/docker-registry-importer/common"
	"github.com/jc-lab/docker-registry-importer/exporter"
	"github.com/jc-lab/docker-registry-importer/importer"
	"github.com/jc-lab/docker-registry-importer/internal/registry"
	"golang.org/x/net/proxy"
	"log"
	"net"
	"net/http"
	"strings"
)

func main() {
	flags := &common.AppFlags{}

	flags.IsImport = flag.Bool("import", false, "import")
	flags.IsExport = flag.Bool("export", false, "export")
	flags.File = flag.String("file", "", "tar file to import")
	flags.Url = flag.String("url", "", "repository address")
	flags.Username = flag.String("username", "", "registry username")
	flags.Password = flag.String("password", "", "registry password")
	flags.Proxy = flag.String("proxy", "", "socks5 proxy")
	flags.IncludeRepoName = flag.Bool("include-repo-name", false, "includeRepoName")
	flags.ConfigFile = flag.String("config", "", "config")
	flags.CacheDir = flag.String("cache-dir", "", "cache directory for export")

	flag.Parse()
	flags.ImageList = flag.Args()

	if flags.ConfigFile != nil && len(*flags.ConfigFile) > 0 {
		config, err := common.ReadConfig(*flags.ConfigFile)
		if err != nil {
			log.Fatalln(err)
		}
		flags.Config = config
	}

	if *flags.IsImport {
		transport := &http.Transport{
			DisableKeepAlives: true,
		}

		if flags.Proxy != nil && len(*flags.Proxy) > 0 {
			dialer, err := proxy.SOCKS5("tcp", *flags.Proxy, nil, proxy.Direct)
			if err != nil {
				log.Fatalln(err)
			}
			transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				return dialer.Dial(network, address)
			}
		}

		url := strings.TrimSuffix(*flags.Url, "/")
		wrappedTransport := registry.WrapTransport(transport, url, *flags.Username, *flags.Username)
		reg := &registry.Registry{
			URL: url,
			Client: &http.Client{
				Transport: wrappedTransport,
			},
			Logf: registry.Log,
		}

		if err := reg.Ping(); err != nil {
			log.Fatalln("ping failed: ", err)
		}

		ctx := &importer.ImportContext{
			Registry: reg,
		}
		ctx.DoImport(flags)
	} else if *flags.IsExport {
		ctx := &exporter.ExportContext{}
		ctx.DoExport(flags)
	}
}
