package exporter

import (
	"archive/tar"
	"crypto"
	"encoding/hex"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/manifestlist"
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

type ImageContext struct {
	reg           *registry.Registry
	leafManifests []distribution.Manifest
}

type ExportContext struct {
	registry map[string]*registry.Registry

	images   []*ImageContext
	blobs    map[string]*ExportBlobItem
	tempDir  string
	cacheDir string
}

type ExportBlobItem struct {
	downloaded    bool
	size          int64
	cachedContent []byte
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

		repo, err := ctx.GetRegistry(registryName, flags.Config)
		if err != nil {
			log.Println(err)
			continue
		}

		manifest, err := repo.ManifestV2(imageName, imageVersion)
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

		// storeManifest
		for _, name := range []string{
			directoryName + "/" + imageVersion,
			directoryName + "/" + digestName,
		} {
			writeToTar(tarWriter, name, payload)
		}

		imageCtx := ImageContext{}
		imageCtx.addManifest(repo, imageName, manifest, tarWriter, directoryName)

		for _, manifest := range imageCtx.leafManifests {
			for _, reference := range manifest.References() {
				ctx.downloadBlob(repo, imageName, tarWriter, reference.Digest, false)
			}
		}
	}

	tarWriter.Flush()
}

func (ctx *ImageContext) addManifest(reg *registry.Registry, imageName string, manifest distribution.Manifest, tarWriter *tar.Writer, tarDirectoryName string) {
	switch typed := manifest.(type) {
	case *manifestlist.DeserializedManifestList:
		for _, descriptor := range typed.ManifestList.Manifests {
			manifest, err := reg.ManifestV2(imageName, descriptor.Digest.String())
			if err != nil {
				log.Fatalln(err)
			}
			_, payload, err := manifest.Payload()
			if err != nil {
				log.Fatalln(err)
			}
			writeToTar(tarWriter, tarDirectoryName+"/"+descriptor.Digest.String(), payload)
			ctx.addManifest(reg, imageName, manifest, tarWriter, tarDirectoryName)
		}
	default:
		ctx.leafManifests = append(ctx.leafManifests, manifest)
	}
}

func writeToTar(tarWriter *tar.Writer, name string, data []byte) {
	err := tarWriter.WriteHeader(&tar.Header{
		Typeflag: tar.TypeReg,
		Name:     name,
		Size:     int64(len(data)),
		Mode:     0644,
		ModTime:  time.Now(),
	})
	if err != nil {
		log.Fatalln(err)
		return
	}
	tarWriter.Write(data)
}

func (ctx *ExportContext) downloadBlob(reg *registry.Registry, repository string, tarWriter *tar.Writer, d digest.Digest, read bool) ([]byte, error) {
	blob := ctx.blobs[d.String()]
	if blob != nil && (!read || (blob.cachedContent != nil && read)) {
		return blob.cachedContent, nil
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
				if read {
					return os.ReadFile(blobFileName)
				}
				return nil, nil
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
		defer os.Remove(blobFileName)
	}

	if read {
		return os.ReadFile(blobFileName)
	}
	return nil, nil
}

func (ctx *ExportContext) GetRegistry(registryName string, config *common.Config) (*registry.Registry, error) {
	reg := ctx.registry[registryName]
	if reg == nil {
		url := "https://" + registryName
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
				if len(repoConfig.Endpoint) > 0 {
					url = strings.TrimSuffix(repoConfig.Endpoint, "/")
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
