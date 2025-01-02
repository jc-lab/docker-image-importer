package registry

import (
	"bytes"
	"errors"
	"github.com/docker/distribution/manifest/manifestlist"
	"github.com/docker/distribution/manifest/ocischema"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
	"github.com/docker/distribution/manifest/schema2"
	digest "github.com/opencontainers/go-digest"
)

func (registry *Registry) Manifest(repository, reference string) (*schema1.SignedManifest, error) {
	url := registry.url("/v2/%s/manifests/%s", repository, reference)
	registry.Logf("registry.manifest.get url=%s repository=%s reference=%s", url, repository, reference)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", schema1.MediaTypeManifest)
	resp, err := registry.Client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	signedManifest := &schema1.SignedManifest{}
	err = signedManifest.UnmarshalJSON(body)
	if err != nil {
		return nil, err
	}

	return signedManifest, nil
}

func (registry *Registry) ManifestV2(repository, reference string) (distribution.Manifest, error) {
	url := registry.url("/v2/%s/manifests/%s", repository, reference)
	registry.Logf("registry.manifest.get url=%s repository=%s reference=%s", url, repository, reference)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", schema2.MediaTypeManifest+", "+v1.MediaTypeImageIndex+", "+v1.MediaTypeImageManifest)
	resp, err := registry.Client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	contentType := resp.Header.Get("content-type")

	var output distribution.Manifest

	switch contentType {
	case manifestlist.MediaTypeManifestList:
		deserialized := &manifestlist.DeserializedManifestList{}
		err = deserialized.UnmarshalJSON(body)
		output = deserialized
	case schema2.MediaTypeManifest:
		deserialized := &schema2.DeserializedManifest{}
		err = deserialized.UnmarshalJSON(body)
		output = deserialized
	case v1.MediaTypeImageIndex:
		deserialized := &manifestlist.DeserializedManifestList{}
		err = deserialized.UnmarshalJSON(body)
		output = deserialized
	case v1.MediaTypeImageManifest:
		deserialized := &ocischema.DeserializedManifest{}
		err = deserialized.UnmarshalJSON(body)
		output = deserialized
	default:
		err = errors.New("invalid response")
	}
	if err != nil {
		return nil, err
	}
	return output, nil
}

func (registry *Registry) ManifestDigest(repository, reference string) (digest.Digest, error) {
	url := registry.url("/v2/%s/manifests/%s", repository, reference)
	registry.Logf("registry.manifest.head url=%s repository=%s reference=%s", url, repository, reference)

	resp, err := registry.Client.Head(url)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return "", err
	}
	return digest.Parse(resp.Header.Get("Docker-Content-Digest"))
}

func (registry *Registry) DeleteManifest(repository string, digest digest.Digest) error {
	url := registry.url("/v2/%s/manifests/%s", repository, digest)
	registry.Logf("registry.manifest.delete url=%s repository=%s reference=%s", url, repository, digest)

	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := registry.Client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return err
	}
	return nil
}

func (registry *Registry) PutManifest(repository, reference string, manifest distribution.Manifest) error {
	url := registry.url("/v2/%s/manifests/%s", repository, reference)
	registry.Logf("registry.manifest.put url=%s repository=%s reference=%s", url, repository, reference)

	mediaType, payload, err := manifest.Payload()
	if err != nil {
		return err
	}

	buffer := bytes.NewBuffer(payload)
	req, err := http.NewRequest("PUT", url, buffer)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", mediaType)
	resp, err := registry.Client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	return err
}
