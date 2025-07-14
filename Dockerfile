FROM gcr.io/distroless/base

# This Dockerfile is used by goreleaser to build multi-arch images to be
# published to public registries for every release.
#
# The binaries are first cross-compiled on the host machine (generally a GH
# Actions worker) and copied into the image directly.
#
# It is not meant to be built manually.
#
# References:
# - .goreleaser.yaml
# - .github/workflows/release.yml
# - Makefile (make release-dry-run)
#
COPY ghavm /usr/local/bin/ghavm

ENTRYPOINT ["/usr/local/bin/ghavm"]
