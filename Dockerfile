# Build the explorer.
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Build the server, statically linked so it runs on a bare image.
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod ./
COPY chain/ ./chain/
COPY cmd/ ./cmd/
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /dyld ./cmd/dyld

# Final image: the binary plus the built UI it serves from ./web/dist.
FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=server /dyld /app/dyld
COPY --from=web /web/dist /app/web/dist
EXPOSE 8080
ENTRYPOINT ["/app/dyld"]
