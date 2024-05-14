package importer

import (
	"archive/tar"
	"encoding/json"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	"github.com/docker/distribution/manifest/schema2"
	"github.com/jc-lab/docker-registry-importer/common"
	"github.com/jc-lab/docker-registry-importer/internal/registry"
	"github.com/jc-lab/docker-registry-importer/pkg/schema1ex"
	"github.com/opencontainers/go-digest"
	"io"
	"log"
	"os"
	"regexp"
)

type ManifestFile struct {
	repository  string
	name        string
	digestType  string
	digestValue string
	tag         string
	data        []byte

	manifest   distribution.Manifest
	descriptor distribution.Descriptor
}

type BlobItem struct {
	uploaded  bool
	manifests []*ManifestFile
	size      int64
}

type ImportContext struct {
	Registry  *registry.Registry
	manifests []*ManifestFile
	blobs     map[string]*BlobItem
}

var regexpManifestFile, _ = regexp.Compile("^(.+)/manifests/([^/:]+):(.+)$")
var regexpTagManifestFile, _ = regexp.Compile("^(.+)/manifests/([^/:]+)$")
var regexpTagFile, _ = regexp.Compile("^(.+)/tags/(.+)$")
var regxpBlobFile, _ = regexp.Compile("^blob/([^/:]+):(.+)$")

func (ctx *ImportContext) DoImport(flags *common.AppFlags) {
	err := ctx.parseArchive(*flags.File)
	if err != nil {
		log.Fatalln(err)
	}

	err = ctx.uploadBlobs(*flags.File)
	if err != nil {
		log.Fatalln(err)
	}

	err = ctx.uploadManifests()
	if err != nil {
		log.Fatalln(err)
	}
}

func (ctx *ImportContext) parseArchive(file string) error {
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

		groups := regexpTagManifestFile.FindStringSubmatch(header.Name)
		if groups != nil {
			repo := groups[1]
			tag := groups[2]

			log.Printf("MANIFEST: " + repo + ":" + tag)

			data, err := io.ReadAll(tarReader)
			if err != nil {
				return err
			}

			item := &ManifestFile{
				repository: repo,
				name:       tag,
				tag:        tag,
				data:       data,
			}
			err = ctx.readManifest(item)
			if err != nil {
				return err
			}
			ctx.manifests = append(ctx.manifests, item)
		}

		groups = regexpManifestFile.FindStringSubmatch(header.Name)
		if groups != nil {
			repo := groups[1]
			digestType := groups[2]
			digestValue := groups[3]

			log.Printf("MANIFEST: " + repo + "@" + digestType + ":" + digestValue)

			data, err := io.ReadAll(tarReader)
			if err != nil {
				return err
			}

			item := &ManifestFile{
				repository:  repo,
				name:        digestType + ":" + digestValue,
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

		//groups = regexpTagFile.FindStringSubmatch(header.Name)
		//if groups != nil {
		//	name := groups[1]
		//	tag := groups[2]
		//
		//	log.Printf("MANIFEST: " + name + ":" + tag)
		//
		//	data, err := io.ReadAll(tarReader)
		//	if err != nil {
		//		return err
		//	}
		//
		//	item := &ManifestFile{
		//		repository: name,
		//		tag:        tag,
		//		data:       data,
		//	}
		//
		//	err = ctx.readManifest(item)
		//	if err != nil {
		//		return err
		//	}
		//	ctx.manifests = append(ctx.manifests, item)
		//}

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
			blob.size, err = common.IoConsumeAll(tarReader)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (ctx *ImportContext) readManifest(item *ManifestFile) error {
	var err error
	var manifestVersion manifest.Versioned
	if err = json.Unmarshal(item.data, &manifestVersion); err != nil {
		return err
	}

	item.manifest, item.descriptor, err = distribution.UnmarshalManifest(manifestVersion.MediaType, item.data)
	if err != nil {
		return err
	}

	switch m := item.manifest.(type) {
	case *schema1ex.DeserializedManifest:
		for _, v := range m.FSLayers {
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

	case *schema2.DeserializedManifest:
		for _, v := range append(m.Layers, m.Config) {
			blob := ctx.blobs[v.Digest.String()]
			if blob == nil {
				blob = &BlobItem{
					manifests: make([]*ManifestFile, 0),
				}
				ctx.blobs[v.Digest.String()] = blob
			}
			blob.manifests = append(blob.manifests, item)
		}

	case *manifestlist.DeserializedManifestList:

	case *ocischema.DeserializedManifest:
		for _, v := range append(m.Layers, m.Config) {
			blob := ctx.blobs[v.Digest.String()]
			if blob == nil {
				blob = &BlobItem{
					manifests: make([]*ManifestFile, 0),
				}
				ctx.blobs[v.Digest.String()] = blob
			}
			blob.manifests = append(blob.manifests, item)
		}

	default:
		log.Printf("UNKNOWN MANIFEST: %s", item.descriptor.MediaType)
	}

	return nil
}

func (ctx *ImportContext) uploadBlobs(file string) error {
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
			if len(blob.manifests) == 0 {
				log.Printf("NO MANIFEST FOR BLOB: %s", digestFull)
				continue
			}
			manifest := blob.manifests[0]

			log.Printf("UPLOAD BLOB: " + digestValue + " (" + manifest.repository + ") START")

			d := digest.NewDigestFromHex(digestType, digestValue)

			has, _ := ctx.Registry.HasBlob(manifest.repository, d)
			if has {
				log.Printf("UPLOAD BLOB: " + d.String() + " (" + manifest.repository + ") ALREADY EXISTS")
			} else {
				err := ctx.Registry.UploadBlob(manifest.repository, d, tarReader, blob.size)
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

func (ctx *ImportContext) uploadManifests() error {
	for _, item := range ctx.manifests {
		fullName := item.repository
		if len(item.tag) > 0 {
			fullName += ":" + item.name
		} else {
			fullName += "@" + item.name
		}

		err := ctx.Registry.PutManifest(item.repository, item.name, item.manifest)
		if err != nil {
			log.Printf("Put Manifest "+fullName+" FAILED: ", err)
			continue
		}

		log.Printf("Put Manifest " + fullName + " SUCCESS")
	}
	return nil
}
