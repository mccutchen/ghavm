FROM gcr.io/distroless/base

# This Dockerfile is by the release.yaml GH Actions workflow to build
# multi-arch images to be published to public registries for every release.
#
# The binaries are first cross-compiled on the host machine by goreleaser and
# copied into the image directly.
#
# It is not meant to be built manually.
#
# References:
# - .goreleaser.yaml
# - .github/workflows/release.yml
# - Makefile (make release-dry-run)
ARG TARGETARCH
COPY dist/ghavm_linux_${TARGETARCH}_*/ghavm /usr/local/bin/ghavm

# Run with, e.g., --volume $(PWD):/src:rw
WORKDIR /src
VOLUME  /src

ENTRYPOINT ["/usr/local/bin/ghavm"]
