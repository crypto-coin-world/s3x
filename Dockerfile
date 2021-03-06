FROM golang:1.15-alpine

LABEL maintainer="RTrade Technologies Ltd <info@rtradetechnologies.com>"

ENV GOPATH /go
ENV CGO_ENABLED 0
ENV GO111MODULE on

RUN  \
     apk add --no-cache git && \
     git clone https://github.com/RTradeLtd/s3x && cd s3x && \
     go install -v -ldflags "$(go run buildscripts/gen-ldflags.go)"

FROM alpine:3.12

ENV MINIO_UPDATE off
ENV MINIO_ACCESS_KEY_FILE=access_key \
    MINIO_SECRET_KEY_FILE=secret_key \
    MINIO_KMS_MASTER_KEY_FILE=kms_master_key \
    MINIO_SSE_MASTER_KEY_FILE=sse_master_key

EXPOSE 9000

COPY --from=0 /go/bin/s3x /usr/bin/minio-s3x
COPY --from=0 /go/s3x/CREDITS /third_party/
COPY --from=0 /go/s3x/dockerscripts/docker-entrypoint.sh /usr/bin/

RUN  \
     apk add --no-cache ca-certificates 'curl>7.61.0' 'su-exec>=0.2' && \
     echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf

ENTRYPOINT ["/usr/bin/docker-entrypoint.sh"]

VOLUME ["/data"]

CMD ["minio-s3x"]
