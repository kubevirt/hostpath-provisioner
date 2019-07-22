FROM alpine:3.4
COPY hostpath-provisioner /
CMD ["/hostpath-provisioner"]
