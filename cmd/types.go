package main

import (
	"github.com/docker/distribution"
	"github.com/docker/distribution/manifest/schema1"
)

type SchemaV1Manifest struct {
	distribution.Manifest
	raw   []byte
	input *schema1.Manifest
}

func fromSchemaV1(raw []byte, input *schema1.Manifest) *SchemaV1Manifest {
	return &SchemaV1Manifest{
		raw:   raw,
		input: input,
	}
}

func (m *SchemaV1Manifest) References() []distribution.Descriptor {
	//m.input.FSLayers
	return make([]distribution.Descriptor, 0)
}

func (m *SchemaV1Manifest) Payload() (mediaType string, payload []byte, err error) {
	return "application/json", m.raw, nil
}
