FROM alpine:3.8

RUN   apk update \
  &&   apk add ca-certificates wget \
  &&   update-ca-certificates

RUN wget 'https://github.com/compendius/libvirt_nova_exporter/releases/download/v1.1-beta/libvirt_nova'
RUN chmod +x ./libvirt_nova
ENTRYPOINT [ "./libvirt_nova" ]
