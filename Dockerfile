FROM gcr.io/distroless/base
COPY ghavm /usr/local/bin/ghavm
ENTRYPOINT ["/usr/local/bin/ghavm"]
