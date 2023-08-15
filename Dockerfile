FROM golang:1.21-bullseye AS build-env

WORKDIR /src

COPY ./go.sum ./go.mod ./

RUN go mod download

COPY . .

RUN mkdir ./output
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -a -installsuffix cgo -ldflags '-extldflags "-static"' -o /src/output ./cmd/...

FROM gcr.io/distroless/static

COPY --from=build-env /src/output /

USER nobody:nobody
ENV PORT=8080
EXPOSE $PORT

HEALTHCHECK --interval=10s --timeout=1s --start-period=5s --retries=3 CMD [ "/health" ]

ENTRYPOINT ["/gokarashta"]
