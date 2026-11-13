# One person in one container. Build from the repository root:
#   docker build -f deploy/Dockerfile -t tinio .
# Or use the one each release publishes: ghcr.io/grandbusta/tinio.
# The person is their database: run it with DATABASE_URL and TINIO_KEY
# (--env-file .env). Nothing about them is in the image, and what every
# person shares is built into the binary.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/tinio ./cmd/tinio

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tinio /tinio
# Listen on all addresses inside the container; publish the ports with -p.
# The console then needs their console token (tinio new makes one).
ENV TINIO_CONSOLE_ADDR=:8090 TINIO_PROFILE_ADDR=:8080
EXPOSE 8080 8090
ENTRYPOINT ["/tinio"]
CMD ["run"]
