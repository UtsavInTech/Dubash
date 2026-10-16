FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dubash ./cmd/dubash \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/events ./cmd/events \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/dubash /app/dubash
COPY --from=build /out/events /app/events
COPY --from=build --chown=65532:65532 /out/data /data
COPY rooms.yaml /app/rooms.yaml
COPY third_party/deepfilternet /app/third_party/deepfilternet
COPY samples/*.wav /app/samples/
EXPOSE 8080
ENTRYPOINT ["/app/dubash", "-addr", ":8080", "-config", "/app/rooms.yaml", "-data", "/data"]
