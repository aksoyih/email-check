FROM golang:1.22-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd

RUN go test ./...
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/email-check ./cmd/api

FROM alpine:3.20

RUN adduser -D -H -u 10001 appuser
USER appuser

COPY --from=build /out/email-check /usr/local/bin/email-check

EXPOSE 8080
ENTRYPOINT ["email-check"]
