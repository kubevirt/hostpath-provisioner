FROM registry.fedoraproject.org/fedora-minimal:30
COPY _out/hostpath-provisioner /
CMD ["/hostpath-provisioner"]
