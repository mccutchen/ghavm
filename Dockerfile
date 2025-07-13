FROM gcr.io/distroless/static-debian12:latest

COPY ghavm /usr/local/bin/ghavm

ENTRYPOINT ["/usr/local/bin/ghavm"]