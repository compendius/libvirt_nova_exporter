FROM alpine:3.8

RUN apk add ca-certificates g++ git go libnl-dev linux-headers make perl pkgconf libtirpc-dev wget

RUN update-ca-certificates
RUN wget ftp://xmlsoft.org/libxml2/libxml2-2.9.4.tar.gz -P /tmp && \
    tar -xf /tmp/libxml2-2.9.4.tar.gz -C /tmp
WORKDIR /tmp/libxml2-2.9.4
RUN ./configure --disable-shared --enable-static && \
    make -j2 && \
    make install
RUN wget https://libvirt.org/sources/libvirt-3.2.0.tar.xz -P /tmp && \
    tar -xf /tmp/libvirt-3.2.0.tar.xz -C /tmp
WORKDIR /tmp/libvirt-3.2.0
RUN ./configure --disable-shared --enable-static --localstatedir=/var --without-storage-mpath && \
    make -j2 && \
    make install && \
    sed -i 's/^Libs:.*/& -lnl -ltirpc -lxml2/' /usr/local/lib/pkgconfig/libvirt.pc






