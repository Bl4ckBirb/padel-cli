FROM golang:1.25-alpine AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=1
RUN go build -trimpath -ldflags="-s -w" -o /out/padel .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata sqlite-libs \
    && addgroup -S padel \
    && adduser -S -G padel -h /home/padel padel \
    && mkdir -p /home/padel/.config/padel \
    && chown -R padel:padel /home/padel
COPY --from=build /out/padel /usr/local/bin/padel
USER padel
WORKDIR /home/padel
ENV PADEL_CONFIG_DIR=/home/padel/.config/padel
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/padel"]
CMD ["serve", "--bind", "0.0.0.0", "--port", "8080"]
