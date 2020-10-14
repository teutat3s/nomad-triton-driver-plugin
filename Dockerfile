FROM alpine:latest

RUN apk add git go linux-headers

WORKDIR /root

RUN git clone https://github.com/teutat3s/nomad-triton-driver-plugin && \
    cd $_ && \
    go get
