FROM alpine:3.8

RUN wget 'https://github.com/compendius/libvirt_nova_exporter/releases/download/v0.5-alpha/libvirt_nova'
RUN chmod +x ./libvirt_nova
ENTRYPOINT [ "./libvirt_nova" ]
