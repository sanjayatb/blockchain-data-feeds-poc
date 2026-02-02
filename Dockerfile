FROM golang:1.21-alpine AS build

RUN apk add --no-cache ca-certificates git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/indexer ./cmd/indexer
RUN CGO_ENABLED=0 go build -o /out/ui ./cmd/ui

FROM alpine:3.19
RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY --from=build /out/indexer /app/indexer
COPY --from=build /out/ui /app/ui
COPY abis /app/abis
COPY configs /app/configs

ENTRYPOINT ["/app/indexer"]
