FROM golang:alpine AS build

# Important:
#   Because this is a CGO enabled package, you are required to set it as 1.
ENV CGO_ENABLED=1

RUN apk add --no-cache \
  # Important: required for go-sqlite3
  gcc \
  # Required for Alpine
  musl-dev

WORKDIR /workspace
COPY . /workspace/

RUN go build -a -o mailsender -ldflags='-s -w -extldflags "-static"'

FROM scratch

COPY --from=build /workspace/mailsender /usr/local/bin/mailsender

ENTRYPOINT [ "/usr/local/bin/mailsender" ]
