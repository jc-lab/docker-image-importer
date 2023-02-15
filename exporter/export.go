package exporter

import (
	"archive/tar"
	"crypto"
	"encoding/hex"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/jc-lab/docker-registry-importer/common"
	"github.com/jc-lab/docker-registry-importer/internal/registry"
	"github.com/opencontainers/go-digest"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type ExportContext struct {
	registry map[string]*registry.Registry

	manifests []*schema2.DeserializedManifest
	blobs     map[string]*ExportBlobItem
	tempDir   string
	cacheDir  string
}

type ExportBlobItem struct {
	downloaded bool
	size       int64
}

func (ctx *ExportContext) DoExport(flags *common.AppFlags) {
	ctx.registry = make(map[string]*registry.Registry)
	ctx.blobs = make(map[string]*ExportBlobItem)

	fileWriter, err := os.OpenFile(*flags.File, os.O_CREATE|os.O_RDWR, 0755)
	if err != nil {
		log.Fatalln(err)
		return
	}
	tarWriter := tar.NewWriter(fileWriter)
	defer tarWriter.Close()
	defer fileWriter.Close()

	ctx.tempDir = os.TempDir()
	ctx.cacheDir = *flags.CacheDir

	if len(ctx.cacheDir) > 0 {
		_ = os.MkdirAll(ctx.cacheDir+"/blob/", 0755)
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

		directoryName := ""
		if *flags.IncludeRepoName {
			directoryName = registryName + "/"
		}
		directoryName += imageName
		directoryName += "/manifests"

		_, payload, _ := manifest.Payload()

		hash := crypto.SHA256.New()
		hash.Write(payload)
		d := hash.Sum(nil)

		digestName := "sha256:" + hex.EncodeToString(d)

		for _, name := range []string{
			directoryName + "/" + imageVersion,
			directoryName + "/" + digestName,
		} {
			err = tarWriter.WriteHeader(&tar.Header{
				Typeflag: tar.TypeReg,
				Name:     name,
				Size:     int64(len(payload)),
				Mode:     0644,
				ModTime:  time.Now(),
			})
			if err != nil {
				log.Fatalln(err)
				return
			}
			tarWriter.Write(payload)
		}

		ctx.manifests = append(ctx.manifests, manifest)

		ctx.downloadBlob(reg, imageName, tarWriter, manifest.Config.Digest)
		for _, layer := range manifest.Layers {
			ctx.downloadBlob(reg, imageName, tarWriter, layer.Digest)
		}
	}

	tarWriter.Flush()
}

func (ctx *ExportContext) downloadBlob(reg *registry.Registry, repository string, tarWriter *tar.Writer, d digest.Digest) {
	blob := ctx.blobs[d.String()]
	if blob != nil {
		return
	}
	blob = &ExportBlobItem{}
	ctx.blobs[d.String()] = blob

	cacheDirUsable := len(ctx.cacheDir) > 0

	blobFileName := ctx.tempDir + "/" + d.String()

	fileToTar := func(filename string, size int64) {
		err := tarWriter.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "blob/" + d.String(),
			Size:     size,
			Mode:     0644,
			ModTime:  time.Now(),
		})
		if err != nil {
			log.Println(err)
			return
		}

		file, err := os.Open(blobFileName)
		if err != nil {
			log.Println(err)
			return
		}
		defer file.Close()

		_, err = io.Copy(tarWriter, file)
		if err != nil {
			log.Println(err)
			return
		}

		blob.downloaded = true
	}

	if cacheDirUsable {
		blobFileName = ctx.cacheDir + "/blob/" + d.String()
		stat, err := os.Stat(blobFileName)
		if err == nil {
			if checkHash(blobFileName, d) {
				fileToTar(blobFileName, stat.Size())
				return
			} else {
				log.Println("cached " + d.String() + " invalid")
			}
		}
	}

	var fileSize int64 = 0

	func() {
		reader, err := reg.DownloadBlob(repository, d)
		if err != nil {
			log.Println(err)
			return
		}
		defer reader.Close()

		file, err := os.OpenFile(blobFileName, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			log.Println(err)
			return
		}
		defer file.Close()

		fileSize, err = file.ReadFrom(reader)
		if err != nil {
			log.Println(err)
			return
		}
	}()

	fileToTar(blobFileName, fileSize)

	if !cacheDirUsable {
		_ = os.Remove(blobFileName)
	}
}

func (ctx *ExportContext) GetRegistry(registryName string, config *common.Config) (*registry.Registry, error) {
	reg := ctx.registry[registryName]
	if reg == nil {
		url := strings.TrimSuffix("https://"+registryName, "/")
		username := ""
		password := ""

		if registryName == "docker.io" {
			url = "https://registry-1.docker.io"
		}

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

func checkHash(filename string, d digest.Digest) bool {
	file, err := os.Open(filename)
	if err != nil {
		return false
	}
	defer file.Close()

	hash := d.Algorithm().Digester().Hash()
	_, err = io.Copy(hash, file)
	if err != nil {
		return false
	}
	return d.Hex() == hex.EncodeToString(hash.Sum(nil))
}
