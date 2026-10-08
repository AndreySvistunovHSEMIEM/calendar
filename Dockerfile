FROM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/orbita . && \
    CGO_ENABLED=0 go build -trimpath -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -o /out/loadgen ./cmd/loadgen

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 orbita && adduser -D -u 10001 -G orbita orbita && \
    mkdir -p /app/data /results && chown -R orbita:orbita /app /results
WORKDIR /app
COPY --from=build /out/ /app/
USER 10001:10001
ENV TZ=Europe/Moscow
EXPOSE 8080 8082 8083
CMD ["/app/orbita"]
