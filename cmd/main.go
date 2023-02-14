package main

import (
	"archive/tar"
	"context"
	"encoding/json"
	"flag"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/jc-lab/docker-image-importer/internal/registry"
	"github.com/opencontainers/go-digest"
	"golang.org/x/net/proxy"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type AppFlags struct {
	url      *string
	proxy    *string
	file     *string
	username *string
	password *string
}

type ManifestFile struct {
	repository  string
	digestType  string
	digestValue string
	tag         string
	data        []byte

	manifestV1 *schema1.Manifest
	manifestV2 *schema2.Manifest
}

type BlobItem struct {
	uploaded  bool
	manifests []*ManifestFile
	size      int64
}

type ArchiveContext struct {
	registry  *registry.Registry
	manifests []*ManifestFile
	blobs     map[string]*BlobItem
}

var regexpManifestFile, _ = regexp.Compile("^(.+)/manifests/([^/:]+):(.+)$")
var regexpTagFile, _ = regexp.Compile("^(.+)/tags/(.+)$")
var regxpBlobFile, _ = regexp.Compile("^blob/([^/:]+):(.+)$")

func main() {
	flags := &AppFlags{}

	flags.url = flag.String("url", "", "repository address")
	flags.proxy = flag.String("proxy", "", "socks5 proxy")
	flags.file = flag.String("file", "", "tar file to import")
	flags.username = flag.String("username", "", "socks5 proxy")
	flags.password = flag.String("password", "", "socks5 proxy")

	flag.Parse()

	transport := &http.Transport{
		DisableKeepAlives: true,
	}

	if flags.proxy != nil && len(*flags.proxy) > 0 {
		dialer, err := proxy.SOCKS5("tcp", *flags.proxy, nil, proxy.Direct)
		if err != nil {
			log.Fatalln(err)
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			return dialer.Dial(network, address)
		}
	}

	url := strings.TrimSuffix(*flags.url, "/")
	wrappedTransport := registry.WrapTransport(transport, url, *flags.username, *flags.password)
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

	ctx := &ArchiveContext{
		registry: reg,
	}

	err := ctx.parseArchive(*flags.file)
	if err != nil {
		log.Fatalln(err)
	}

	err = ctx.uploadBlobs(*flags.file)
	if err != nil {
		log.Fatalln(err)
	}

	err = ctx.uploadManifests()
	if err != nil {
		log.Fatalln(err)
	}
}

func (ctx *ArchiveContext) parseArchive(file string) error {
	ctx.manifests = make([]*ManifestFile, 0)
	ctx.blobs = make(map[string]*BlobItem)

	reader, err := os.OpenFile(file, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer reader.Close()

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF || header == nil {
			break
		} else if err != nil {
			return err
		}

		groups := regexpManifestFile.FindStringSubmatch(header.Name)
		if groups != nil {
			name := groups[1]
			digestType := groups[2]
			digestValue := groups[3]

			log.Printf("MANIFEST: " + name + "@" + digestType + ":" + digestValue)

			data, err := io.ReadAll(tarReader)
			if err != nil {
				return err
			}

			item := &ManifestFile{
				repository:  name,
				digestType:  digestType,
				digestValue: digestValue,
				data:        data,
			}
			err = ctx.readManifest(item)
			if err != nil {
				return err
			}
			ctx.manifests = append(ctx.manifests, item)
		}
		groups = regexpTagFile.FindStringSubmatch(header.Name)
		if groups != nil {
			name := groups[1]
			tag := groups[2]

			log.Printf("MANIFEST: " + name + ":" + tag)

			data, err := io.ReadAll(tarReader)
			if err != nil {
				return err
			}

			item := &ManifestFile{
				repository: name,
				tag:        tag,
				data:       data,
			}

			err = ctx.readManifest(item)
			if err != nil {
				return err
			}
			ctx.manifests = append(ctx.manifests, item)
		}
		groups = regxpBlobFile.FindStringSubmatch(header.Name)
		if groups != nil {
			digestType := groups[1]
			digestValue := groups[2]
			digestFull := digestType + ":" + digestValue
			blob := ctx.blobs[digestFull]
			if blob == nil {
				blob = &BlobItem{
					manifests: make([]*ManifestFile, 0),
				}
				ctx.blobs[digestFull] = blob
			}
			blob.size, err = ioConsumeAll(tarReader)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (ctx *ArchiveContext) readManifest(item *ManifestFile) error {
	var manifestVersion manifest.Versioned
	err := json.Unmarshal(item.data, &manifestVersion)
	if err != nil {
		return err
	}

	switch manifestVersion.SchemaVersion {
	case 1:
		item.manifestV1 = &schema1.Manifest{}
		err = json.Unmarshal(item.data, item.manifestV1)
		if err != nil {
			return err
		}
		for _, v := range item.manifestV1.FSLayers {
			blob := ctx.blobs[v.BlobSum.String()]
			if blob == nil {
				blob = &BlobItem{
					manifests: make([]*ManifestFile, 0),
				}
				ctx.blobs[v.BlobSum.String()] = blob
			}
			blob.manifests = append(blob.manifests, item)
		}

		//packedManifest = fromSchemaV1(item.data, item.manifestV1)
		break
	case 2:
		item.manifestV2 = &schema2.Manifest{}
		err = json.Unmarshal(item.data, item.manifestV2)
		if err != nil {
			return err
		}

		for _, v := range append(item.manifestV2.Layers, item.manifestV2.Config) {
			blob := ctx.blobs[v.Digest.String()]
			if blob == nil {
				blob = &BlobItem{
					manifests: make([]*ManifestFile, 0),
				}
				ctx.blobs[v.Digest.String()] = blob
			}
			blob.manifests = append(blob.manifests, item)
		}
		break
	}

	return nil
}

func (ctx *ArchiveContext) uploadBlobs(file string) error {
	reader, err := os.OpenFile(file, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer reader.Close()

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF || header == nil {
			break
		} else if err != nil {
			return err
		}

		groups := regxpBlobFile.FindStringSubmatch(header.Name)
		if groups != nil {
			digestType := groups[1]
			digestValue := groups[2]
			digestFull := digestType + ":" + digestValue
			blob := ctx.blobs[digestFull]
			if blob == nil {
				log.Printf("empty blob: " + digestValue)
				continue
			}
			manifest := blob.manifests[0]

			log.Printf("UPLOAD BLOB: " + digestValue + " (" + manifest.repository + ") START")

			d := digest.NewDigestFromHex(digestType, digestValue)

			has, _ := ctx.registry.HasBlob(manifest.repository, d)
			if has {
				log.Printf("UPLOAD BLOB: " + d.String() + " (" + manifest.repository + ") ALREADY EXISTS")
			} else {
				err := ctx.registry.UploadBlob(manifest.repository, d, tarReader, blob.size)
				if err == nil {
					log.Printf("UPLOAD BLOB: " + d.String() + " (" + manifest.repository + ") SUCCESS")
				} else {
					log.Printf("UPLOAD BLOB: " + d.String() + " (" + manifest.repository + ") FAILED: " + err.Error())
				}
			}
		}
	}

	return nil
}

func (ctx *ArchiveContext) uploadManifests() error {
	for _, item := range ctx.manifests {
		if item.manifestV2 != nil {
			fullName := item.repository
			if len(item.tag) > 0 {
				fullName += ":" + item.tag
			} else {
				fullName += "@" + item.digestType + ":" + item.digestValue
			}

			m := &schema2.DeserializedManifest{}
			err := m.UnmarshalJSON(item.data)
			if err != nil {
				log.Printf("Put Manifest "+fullName+" FAILED: ", err)
				continue
			}

			err = ctx.registry.PutManifest(item.repository, item.digestType+":"+item.digestValue, m)
			if err != nil {
				log.Printf("Put Manifest "+fullName+" FAILED: ", err)
				continue
			}

			log.Printf("Put Manifest " + fullName + " SUCCESS")
		}
	}
	return nil
}

func ioConsumeAll(reader io.Reader) (int64, error) {
	buf := make([]byte, 1024)
	var totalBytes int64 = 0
	n := 1
	for n >= 0 {
		var err error
		n, err = reader.Read(buf)
		if n > 0 {
			totalBytes += int64(n)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return totalBytes, err
		}
	}
	return totalBytes, nil
}
