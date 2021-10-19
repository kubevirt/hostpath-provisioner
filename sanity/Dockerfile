FROM registry.fedoraproject.org/fedora-minimal:34
COPY _out/sanity.test /
RUN microdnf install -y tar bc zstd

ENTRYPOINT ["/sanity.test"]

