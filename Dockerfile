# Use the official Golang image to create a build artifact.
FROM golang:1.21-bookworm AS build-env

# Set the working directory outside $GOPATH to enable the support for modules.
WORKDIR /src

# Fetch dependencies first; they are less susceptible to change on every build
# and will therefore be cached for speeding up the next build.
COPY ./go.sum ./go.mod ./
RUN go mod download

# Copy the local package files to the container's workspace.
COPY . .

# Install ImageMagick.
RUN apt-get update && apt-get install -y imagemagick

# Build the application inside the container.
RUN mkdir ./output
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /src/output ./cmd/...

# Use Debian Bookworm for the final image.
FROM debian:bookworm

# Copy the binary to the production image from the builder stage.
COPY --from=build-env /src/output/gokarashta /app/gokarashta

# Install ImageMagick in the final image.
RUN apt-get update && apt-get install -y imagemagick && rm -rf /var/lib/apt/lists/*

# Use the unprivileged user.
USER nobody:nobody
ENV PORT=8080
EXPOSE $PORT

HEALTHCHECK --interval=10s --timeout=1s --start-period=5s --retries=3 CMD [ "/app/health" ]

ENTRYPOINT ["/app/gokarashta"]

