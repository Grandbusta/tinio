# The platform's backend: one image, two ways to run it. Build from the
# repository root:
#   docker build -f deploy/Dockerfile -t tinio .
# Run the API with DATABASE_URL and SIGNIN_SECRET, and the worker (which runs
# the agents) with the same image and `worker`, DATABASE_URL, OPENAI_API_KEY
# and OPENAI_MODEL. What every agent shares is built into the binary;
# everything about an agent is in the database.
FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/tinio ./api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tinio /tinio
ENV PORT=8081
EXPOSE 8081
ENTRYPOINT ["/tinio"]
