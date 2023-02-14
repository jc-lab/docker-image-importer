# docker-registry-importer

docker-registry-importer is a tool to import images from a tar (not a file saved with docker save!) file.

# Usage

```text
Usage of docker-registry-importer:
  -file string
        tar file to import
  -url string
        registry address (e.g. http://docker-registry.io/v2/)
  -username string
        registry username
  -password string
        registry password
  -proxy string
        socks5 proxy (e.g. 1.2.3.4:1234)
```

# tar archive structure

```
library/something/manifests/v1.2.3
repo/name/manifests/TAG_NAME
...
library/something/manifests/sha256:e372ed08ad996742c98b2bf83df787ac26cb1062063986db65c2fe5b34a35fe2
library/something/manifests/sha256:DIGEST
...
blob/sha256:284842a36c0d8eea230cfd5e7a4a6b450fcd63d1c4737f236a91e1671455050a
blob/sha256:3cca8e8510b3d56a64390c3328b31be3a09171557044c1e6431e7bf6ba90f255
blob/sha256:DIGEST
...
```

# License

[Apache-2.0](./LICENSE)
