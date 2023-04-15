FROM golang:alpine AS build

WORKDIR /workspace
COPY . /workspace/

RUN go build -a -o mailsender -ldflags='-s -w -extldflags "-static"'

FROM scratch

COPY --from=build /workspace/mailsender /usr/local/bin/mailsender

ENTRYPOINT [ "/usr/local/bin/mailsender" ]
