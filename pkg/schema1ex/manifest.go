package schema1ex

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
)

// DeserializedManifest wraps Manifest with a copy of the original JSON.
// It satisfies the distribution.Manifest interface.
type DeserializedManifest struct {
	schema1.Manifest

	// canonical is the canonical byte representation of the Manifest.
	canonical []byte
}

func (m *DeserializedManifest) References() []distribution.Descriptor {
	return make([]distribution.Descriptor, 0)
}

// UnmarshalJSON populates a new Manifest struct from JSON data.
func (m *DeserializedManifest) UnmarshalJSON(b []byte) error {
	m.canonical = make([]byte, len(b))
	// store manifest in canonical
	copy(m.canonical, b)

	// Unmarshal canonical JSON into Manifest object
	var manifest schema1.Manifest
	if err := json.Unmarshal(m.canonical, &manifest); err != nil {
		return err
	}

	if manifest.MediaType != schema1.MediaTypeManifest {
		return fmt.Errorf("mediaType in manifest should be '%s' not '%s'",
			schema1.MediaTypeManifest, manifest.MediaType)
	}

	m.Manifest = manifest

	return nil
}

// MarshalJSON returns the contents of canonical. If canonical is empty,
// marshals the inner contents.
func (m *DeserializedManifest) MarshalJSON() ([]byte, error) {
	if len(m.canonical) > 0 {
		return m.canonical, nil
	}

	return nil, errors.New("JSON representation not initialized in DeserializedManifest")
}

// Payload returns the raw content of the manifest. The contents can be used to
// calculate the content identifier.
func (m DeserializedManifest) Payload() (string, []byte, error) {
	return m.MediaType, m.canonical, nil
}
