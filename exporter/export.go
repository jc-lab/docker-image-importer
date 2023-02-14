package exporter

import (
	"crypto"
	"encoding/hex"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/jc-lab/docker-registry-importer/common"
	"github.com/jc-lab/docker-registry-importer/internal/registry"
	"github.com/opencontainers/go-digest"
	"log"
	"net/http"
	"os"
	"strings"
)

type ExportContext struct {
	registry map[string]*registry.Registry

	manifests []*schema2.DeserializedManifest
	blobs     map[string]*ExportBlobItem
	outputDir string
	blobDir   string
}

type ExportBlobItem struct {
	downloaded bool
	size       int64
}

func (ctx *ExportContext) DoExport(flags *common.AppFlags) {
	ctx.registry = make(map[string]*registry.Registry)
	ctx.blobs = make(map[string]*ExportBlobItem)

	ctx.outputDir = "./output"
	ctx.blobDir = ctx.outputDir + "/blob"

	err := os.MkdirAll(ctx.blobDir, 0755)
	if err != nil {
		log.Println(err)
		return
	}

	for _, imageName := range flags.ImageList {
		tokens := strings.SplitN(imageName, "/", 2)
		registryName := tokens[0]
		tokens = strings.SplitN(tokens[1], ":", 2)
		imageName := tokens[0]
		imageVersion := tokens[1]

		log.Println(registryName + " " + imageName + " " + imageVersion)

		reg, err := ctx.GetRegistry(registryName, flags.Config)
		if err != nil {
			log.Println(err)
			continue
		}

		manifest, err := reg.ManifestV2(imageName, imageVersion)
		if err != nil {
			log.Println(err)
			continue
		}

		directoryName := ctx.outputDir
		if *flags.IncludeRepoName {
			directoryName += "/" + registryName + "/"
		}
		directoryName += imageName
		directoryName += "/manifests"
		err = os.MkdirAll(directoryName, 0755)
		if err != nil {
			log.Println(err)
			continue
		}

		_, payload, _ := manifest.Payload()

		hash := crypto.SHA256.New()
		hash.Write(payload)
		digest := hash.Sum(nil)

		digestName := "sha256:" + hex.EncodeToString(digest)

		os.WriteFile(directoryName+"/"+imageVersion, payload, 0644)
		os.WriteFile(directoryName+"/"+digestName, payload, 0644)

		ctx.manifests = append(ctx.manifests, manifest)

		ctx.downloadBlob(reg, imageName, manifest.Config.Digest)
		for _, layer := range manifest.Layers {
			ctx.downloadBlob(reg, imageName, layer.Digest)
		}
	}
}

func (ctx *ExportContext) downloadBlob(reg *registry.Registry, repository string, d digest.Digest) {
	blob := ctx.blobs[d.String()]
	if blob != nil {
		return
	}
	blob = &ExportBlobItem{}
	ctx.blobs[d.String()] = blob

	reader, err := reg.DownloadBlob(repository, d)
	if err != nil {
		log.Println(err)
		return
	}

	defer reader.Close()

	blobFileName := ctx.blobDir + "/" + d.String()
	file, err := os.OpenFile(blobFileName, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Println(err)
		return
	}

	_, err = file.ReadFrom(reader)
	if err != nil {
		log.Println(err)
		return
	}

	blob.downloaded = true
}

func (ctx *ExportContext) GetRegistry(registryName string, config *common.Config) (*registry.Registry, error) {
	reg := ctx.registry[registryName]
	if reg == nil {
		url := strings.TrimSuffix("https://"+registryName, "/")
		username := ""
		password := ""

		transport := &http.Transport{
			DisableKeepAlives: true,
		}

		if config != nil {
			repoConfig := config.Repositories[registryName]
			if repoConfig != nil {
				if len(repoConfig.Username) > 0 || len(repoConfig.Password) > 0 {
					username = repoConfig.Username
					password = repoConfig.Password
				}
			}
		}
		wrappedTransport := registry.WrapTransport(transport, url, username, password)
		reg = &registry.Registry{
			URL: url,
			Client: &http.Client{
				Transport: wrappedTransport,
			},
			Logf: registry.Log,
		}
		ctx.registry[registryName] = reg
	}

	return reg, nil
}
