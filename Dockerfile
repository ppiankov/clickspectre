# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

RUN apk add --no-cache upx

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
	-trimpath \
	-ldflags="-s -w -X main.version=${VERSION}" \
	-o /out/clickspectre \
	./cmd/clickspectre && \
	upx --best --lzma /out/clickspectre

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/clickspectre /usr/local/bin/clickspectre

USER nonroot:nonroot

ENTRYPOINT ["/usr/local/bin/clickspectre"]
